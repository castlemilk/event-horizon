package usb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// LinkStateRecord mirrors what the `cmdctl link` CLI writes when its
// bridge is running. The CLI holds the USB device exclusively while the
// link is up, so the daemon's own bus scan reports no dongle for exactly
// the period when the link is working. This record is the daemon's only
// window into that link; it is read as a FALLBACK when the bus scan is
// empty, never as an authority that overrides a real USB sighting.
type LinkStateRecord struct {
	SSID      string    `json:"ssid"`
	Iface     string    `json:"iface"`
	IP        string    `json:"ip"`
	Gateway   string    `json:"gateway,omitempty"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

var (
	linkStateMu     sync.Mutex
	linkStateParsed bool
	linkStateVal    LinkStateFileView
	linkStateAt     time.Time
)

// LinkStateFileView is the daemon-facing projection: the record plus a
// liveness verdict. Alive is true only when the writing process still
// exists and the entry is fresh.
type LinkStateFileView struct {
	Record LinkStateRecord
	Alive  bool
}

const linkStateMaxAge = 24 * time.Hour

// ReadDongleLinkState reads ~/.event-horizon/linkstate.json. Cached for
// two seconds: the status endpoint polls faster than a file read needs
// to repeat, and the record changes at most once per link lifetime.
// A dead PID or stale entry reads as not-alive; a missing file as
// zero-value with Alive false. Every error path degrades to "no link
// recorded", never to an error — this is a fallback view, not a fact.
func ReadDongleLinkState(now time.Time) LinkStateFileView {
	linkStateMu.Lock()
	defer linkStateMu.Unlock()
	if linkStateParsed && now.Sub(linkStateAt) < 2*time.Second {
		return linkStateVal
	}
	linkStateParsed = true
	linkStateAt = now

	var rec LinkStateRecord
	p := linkStatePath()
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &rec) != nil {
		linkStateVal = LinkStateFileView{}
		return linkStateVal
	}
	alive := rec.PID > 0 && processExists(rec.PID) && now.Sub(rec.StartedAt) <= linkStateMaxAge
	linkStateVal = LinkStateFileView{Record: rec, Alive: alive}
	return linkStateVal
}

// InvalidateLinkStateCache drops the cached read; tests use it to keep
// stubbed writes visible across calls.
func InvalidateLinkStateCache() {
	linkStateMu.Lock()
	defer linkStateMu.Unlock()
	linkStateParsed = false
}

func linkStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".event-horizon", "linkstate.json")
	}
	return filepath.Join(home, ".event-horizon", "linkstate.json")
}

func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
