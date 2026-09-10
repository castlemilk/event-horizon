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

	go func() {
		defer close(done)
		rc := l.run(ctx, opts)
		l.mu.Lock()
		l.cancel = nil
		if rc != 0 && l.status.State != LinkNeedsReplug && l.status.State != LinkNoDongle {
			l.status.State = LinkFailed
			if l.status.Detail == "" {
				l.status.Detail = "bring-up failed; see the daemon log"
			}
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

// LinkOptions are the parameters of one bring-up.
type LinkOptions struct {
	SSID    string `json:"ssid"`
	Pass    string `json:"pass"`
	Channel int    `json:"channel"`
	BSSID   string `json:"bssid"`
	Route   string `json:"route"`
}

// run drives the same code path as `cmdctl link`, so the API and the CLI
// cannot drift into behaving differently.
func (l *LinkService) run(ctx context.Context, opts LinkOptions) int {
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
	l.set(LinkAssociating, "flashing if needed, then associating with "+opts.SSID)
	rc := runCmdLink(ctx, args)
	if rc == 0 {
		l.set(LinkUp, "link is up")
	}
	return rc
}
