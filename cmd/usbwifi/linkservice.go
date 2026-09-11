package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// LinkState is where the dongle is in the bring-up sequence.
//
// The states are the ones the hardware actually imposes, not a tidy
// abstraction over them: this chip only re-enters a flashable mode on a
// physical power cycle, so "needs a replug" is a real state a caller has to be
// told about rather than something the software can retry its way out of.
type LinkState string

const (
	LinkIdle        LinkState = "idle"         // nothing attempted yet
	LinkNoDongle    LinkState = "no_dongle"    // nothing on the USB bus
	LinkNeedsReplug LinkState = "needs_replug" // firmware already used; only a VBUS drop clears it
	LinkFlashing    LinkState = "flashing"     // ZeroCD/boot ROM -> operational
	LinkAssociating LinkState = "associating"  // stack up, joining the BSS
	LinkHandshaking LinkState = "handshaking"  // WPA2 4-way
	LinkConfiguring LinkState = "configuring"  // DHCP + bridge
	LinkUp          LinkState = "up"           // carrying traffic
	LinkDown        LinkState = "down"         // was up, then dropped
	LinkFailed      LinkState = "failed"       // gave up; Detail says why
)

// LinkStatus is the observable state of the dongle link.
type LinkStatus struct {
	State LinkState `json:"state"`
	// Detail is the human-readable reason, and for the states a caller must
	// act on (needs_replug, failed) it is the instruction.
	Detail    string    `json:"detail"`
	SSID      string    `json:"ssid,omitempty"`
	Since     time.Time `json:"since"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError,omitempty"`
}

// LinkService owns the dongle link inside the daemon, so the link can be
// driven over the API (and therefore over MCP) rather than only from a
// terminal.
//
// One link at a time, deliberately: the radio is a single physical device and
// two concurrent bring-ups would fight over the USB session, which is exactly
// the failure mode that costs a replug.
type LinkService struct {
	mu     sync.Mutex
	status LinkStatus
	cancel context.CancelFunc
	done   chan struct{}
}

func NewLinkService() *LinkService {
	return &LinkService{status: LinkStatus{State: LinkIdle, Detail: "no link attempted", Since: time.Now()}}
}

// Status returns the current state.
func (l *LinkService) Status() LinkStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

func (l *LinkService) set(state LinkState, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.status.State = state
	l.status.Detail = detail
	l.status.Since = time.Now()
}

// Start brings the link up in the background and returns immediately with the
// state at that moment. Callers poll Status.
//
// It refuses rather than queues when a link is already running: silently
// replacing a working link because a second caller asked would be worse than
// saying no.
func (l *LinkService) Start(opts LinkOptions) (LinkStatus, error) {
	l.mu.Lock()
	if l.cancel != nil {
		st := l.status
		l.mu.Unlock()
		return st, fmt.Errorf("a link is already active (%s); stop it first", st.State)
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.done = make(chan struct{})
	l.status = LinkStatus{
		State: LinkFlashing, Detail: "starting bring-up", SSID: opts.SSID,
		Since: time.Now(), Attempts: l.status.Attempts + 1,
	}
	done := l.done
	st := l.status
	l.mu.Unlock()

	linkProgress = func(state LinkState, detail string) { l.set(state, detail) }
	go func() {
		defer close(done)
		defer func() { linkProgress = nil }()
		rc := l.run(ctx, opts)
		l.mu.Lock()
		l.cancel = nil
		switch {
		case ctx.Err() != nil:
			// A deliberate Stop(). It sets idle once run() returns, so leave the
			// state alone rather than racing it to a different answer.

		case rc != 0:
			// Including from needs_replug: once the wait is over, continuing to
			// display "unplug the dongle" is a stale instruction for something
			// nobody is still waiting on.
			if l.status.State == LinkNeedsReplug {
				l.status.Detail = "gave up waiting for the power cycle; start the link again"
			} else if l.status.Detail == "" {
				l.status.Detail = "bring-up failed; see the daemon log"
			}
			l.status.State = LinkFailed

		default:
			// run() returning AT ALL means the link is over. A zero exit code
			// says the bridge shut down cleanly — not that it is still carrying
			// traffic — and this branch used to do nothing, so the state stayed
			// on whatever it last reached, which for a working link is "up".
			//
			// That is not hypothetical. Pulling the dongle exits the bridge
			// cleanly, and the service went on reporting "bridged on utun11 as
			// 192.168.2.243" for hardware that was no longer plugged in. The
			// utun device and its host route outlive the radio, so nothing
			// downstream noticed either: the dish just became unreachable while
			// the link insisted it was up.
			l.status.State = LinkDown
			l.status.Detail = "the link ended — the dongle was unplugged, or the bridge exited"
		}
		l.mu.Unlock()
	}()
	return st, nil
}

// Stop tears the link down and waits for it to finish.
func (l *LinkService) Stop() {
	l.mu.Lock()
	cancel, done := l.cancel, l.done
	l.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
	l.set(LinkIdle, "stopped")
}

// linkProgress is how the bring-up stages report where they are.
//
// runCmdLink blocks for the life of the link and prints to stdout, so without
// this the service could only report the state it set before calling in — it
// sat on "associating" while the link was up and carrying traffic. A state
// machine that cannot see its own transitions is worse than no state machine:
// it reports confidently and wrongly.
var linkProgress func(LinkState, string)

func reportLink(state LinkState, detail string) {
	if linkProgress != nil {
		linkProgress(state, detail)
	}
}

// LinkOptions are the parameters of one bring-up.
type LinkOptions struct {
	SSID    string `json:"ssid"`
	Pass    string `json:"pass"`
	Channel int    `json:"channel"`
	BSSID   string `json:"bssid"`
	Route   string `json:"route"`
	// WaitReplug is how long to wait for the user to power-cycle the dongle
	// when the chip needs it. Defaults to two minutes.
	WaitReplug time.Duration `json:"waitReplug"`
}

// run drives the same code path as `cmdctl link`, so the API and the CLI
// cannot drift into behaving differently.
func (l *LinkService) run(ctx context.Context, opts LinkOptions) int {
	// Default before building the arguments, not after — an earlier version
	// applied it afterwards and passed "0s", so the API path dead-ended on a
	// console instruction nobody could see instead of waiting for the replug.
	if opts.WaitReplug <= 0 {
		// Generous on purpose: this waits on a person walking to the machine
		// and pulling a plug. Two minutes looked reasonable and was not — the
		// window expired before the replug, and the run failed for no reason
		// other than the clock.
		opts.WaitReplug = 15 * time.Minute
	}
	args := []string{"--ssid", opts.SSID}
	if opts.Pass != "" {
		args = append(args, "--pass", opts.Pass)
	}
	if opts.Channel != 0 {
		args = append(args, "--channel", fmt.Sprint(opts.Channel))
	}
	if opts.BSSID != "" {
		args = append(args, "--bssid", opts.BSSID)
	}
	if opts.Route != "" {
		args = append(args, "--route", opts.Route)
	}
	// Over the API nobody is watching a console, so wait for the power cycle
	// rather than returning an instruction no one will read. The state is
	// reported as needs_replug throughout, so a UI can prompt.
	args = append(args, "--wait-replug", opts.WaitReplug.String())
	l.set(LinkAssociating, "flashing if needed, then associating with "+opts.SSID)
	rc := runCmdLink(ctx, args)
	if rc == 0 {
		l.set(LinkUp, "link is up")
	}
	return rc
}
