package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LinkStateFile is the handshake between the `cmdctl link` CLI process and
// the long-running daemon.
//
// The two are separate processes by design: the CLI must hold the USB
// device exclusively (libusb claims it), which means the daemon's own bus
// scan cannot see the dongle while a link is up — it reports NO_DONGLE,
// truthfully about its own view, while utun11 carries traffic. Before
// this file existed that was the remaining dishonesty in the status
// endpoint: a working link read as "nothing connected".
//
// The CLI writes this file when the bridge comes up and removes it when
// the bridge exits (clean stop or firmware-reported disconnect). The
// daemon reads it ONLY as a fallback when its bus scan sees no dongle —
// a file that outlived its process is stale by definition, so a dead
// CLI's entry is dropped after a generous grace period and never
// presented as current.
type LinkStateFile struct {
	SSID      string    `json:"ssid"`
	Iface     string    `json:"iface"`
	IP        string    `json:"ip"`
	Gateway   string    `json:"gateway,omitempty"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

// linkStateMu serialises read-modify-write on the state file. The CLI is
// the only writer and the daemon the only reader, but tests (and a
// careless second CLI) can race the remove/write pair without this.
var linkStateMu sync.Mutex

func linkStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".event-horizon", "linkstate.json")
	}
	return filepath.Join(home, ".event-horizon", "linkstate.json")
}

// WriteLinkState atomically records a live bridge. Called by the CLI when
// the bridge reports running.
func WriteLinkState(st LinkStateFile) error {
	linkStateMu.Lock()
	defer linkStateMu.Unlock()
	p := linkStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// RemoveLinkState clears the record. Called when the bridge exits for
// any reason.
func RemoveLinkState() {
	linkStateMu.Lock()
	defer linkStateMu.Unlock()
	_ = os.Remove(linkStatePath())
}

// ReadLinkState returns the recorded link state when it plausibly
// describes a live bridge: the writing process must still exist and the
// entry must be younger than linkStateMaxAge. Any other case — missing
// file, dead PID, stale timestamp — reads as "no link".
func ReadLinkState(now time.Time) (LinkStateFile, bool) {
	linkStateMu.Lock()
	defer linkStateMu.Unlock()
	var st LinkStateFile
	b, err := os.ReadFile(linkStatePath())
	if err != nil || json.Unmarshal(b, &st) != nil {
		return LinkStateFile{}, false
	}
	if st.PID <= 0 || !processAlive(st.PID) {
		return LinkStateFile{}, false
	}
	if now.Sub(st.StartedAt) > linkStateMaxAge {
		return LinkStateFile{}, false
	}
	return st, true
}

// linkStateMaxAge bounds how long an entry can be trusted even if its
// process is somehow still alive: a bridge that has not refreshed in
// this window is treated as gone. Generous — the CLI is expected to be
// alive for the whole bridge lifetime — but not unbounded.
const linkStateMaxAge = 24 * time.Hour

