package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"os"
	"sync"
)

// Daemon identity, so the app can tell its own daemon from somebody else's.
//
// The app ships the daemon inside its bundle and is responsible for its
// lifetime, but nothing ever checked that the daemon answering on :8990 was the
// one the app shipped. In practice it repeatedly was not: a daemon started from
// an older bundle kept running, a newly built app found the port answering,
// concluded all was well, and left a stale binary serving. Every fix made to
// the daemon looked like it had no effect, because it had none — the running
// process predated it.
//
// A build fingerprint makes that detectable. The daemon hashes its own
// executable at startup and reports it; the app hashes the binary in its bundle
// and compares. Equal means the running daemon IS the bundled one. Different
// means restart it, which is a decision the app can now make on its own instead
// of a discrepancy nobody could see.

var (
	fingerprintOnce sync.Once
	fingerprint     string
)

// BuildFingerprint is the SHA-256 of this process's own executable.
//
// The executable is hashed rather than a version string being compiled in,
// because a version string is only as good as someone's discipline in bumping
// it — and the failure this exists to catch is precisely two builds that both
// call themselves 1.0.0.
func BuildFingerprint() string {
	fingerprintOnce.Do(func() {
		path, err := os.Executable()
		if err != nil {
			log.Printf("[API] cannot locate own executable for fingerprinting: %v", err)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			log.Printf("[API] cannot read own executable for fingerprinting: %v", err)
			return
		}
		defer f.Close()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			log.Printf("[API] cannot hash own executable: %v", err)
			return
		}
		fingerprint = hex.EncodeToString(h.Sum(nil))
	})
	return fingerprint
}

// ExecutablePath reports where this daemon was launched from, so a mismatch can
// be explained rather than merely detected — "the daemon on :8990 is running
// from a different bundle" is actionable in a way that a hash difference is not.
func ExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}
