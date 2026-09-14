package usb

import (
	"sync"
	"testing"
	"time"
)

// stubEnumerate swaps the cache's USB pass for a scripted one. The mutex
// exists because the pass runs on a background goroutine the test does not
// own — without it -race blames the test, correctly.
type stubEnumerate struct {
	mu    sync.Mutex
	fn    func() []DeviceInfo
	calls int
}

func (s *stubEnumerate) install() func() {
	old := enumerateDongles
	enumerateDongles = func() []DeviceInfo {
		// Count under lock, run outside it: the stubbed pass may block
		// (that is the wedged-pass test) and holding the mutex across
		// the call would deadlock the counter read below.
		s.mu.Lock()
		s.calls++
		fn := s.fn
		s.mu.Unlock()
		return fn()
	}
	return func() {
		// A pass spawned by this test may still be in flight; restoring
		// the seam underneath it races the next test's install. The
		// wedged test closes its release first (LIFO defers), so every
		// pass here terminates — wait for that, bounded.
		deadline := time.Now().Add(5 * time.Second)
		for {
			dongleCacheMu.Lock()
			busy := dongleCacheRefreshing
			dongleCacheMu.Unlock()
			if !busy || time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		enumerateDongles = old
	}
}

func (s *stubEnumerate) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func waitProbed(t *testing.T, timeout time.Duration) []DeviceInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		devs, probed := CachedDongles()
		if probed {
			return devs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cache never completed a pass")
	return nil
}

// Cold cache claims nothing: no dongles and, critically, probed=false so
// callers say "probing" rather than "absent".
func TestCachedDonglesColdIsNotAbsent(t *testing.T) {
	resetDongleCache()
	stub := &stubEnumerate{fn: func() []DeviceInfo { return nil }}
	defer stub.install()()

	start := time.Now()
	devs, probed := CachedDongles()
	if time.Since(start) > 2*time.Second {
		t.Fatal("cold read blocked: the whole point of the cache is it never does")
	}
	if probed || len(devs) != 0 {
		t.Fatalf("cold cache = (%v, %v), want ([], false)", devs, probed)
	}
}

// A wedged USB pass must not wedge callers: reads keep returning
// immediately until the pass comes back.
func TestCachedDonglesSurvivesWedgedPass(t *testing.T) {
	resetDongleCache()
	release := make(chan struct{})
	stub := &stubEnumerate{fn: func() []DeviceInfo {
		<-release
		return []DeviceInfo{{Name: "late", IsWlan: true}}
	}}
	defer stub.install()()
	defer close(release)

	for i := 0; i < 5; i++ {
		start := time.Now()
		if _, probed := CachedDongles(); probed {
			t.Fatal("wedged pass reported probed")
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("read %d blocked on a wedged pass", i)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n := stub.callCount(); n != 1 {
		t.Fatalf("expected exactly one background pass, got %d", n)
	}
}

// Once a pass lands, reads serve it without re-enumerating until the TTL.
func TestCachedDonglesServesWarmWithoutRepass(t *testing.T) {
	oldTTL := dongleCacheTTL
	dongleCacheTTL = 300 * time.Millisecond
	defer func() { dongleCacheTTL = oldTTL }()

	resetDongleCache()
	stub := &stubEnumerate{fn: func() []DeviceInfo {
		return []DeviceInfo{{Name: "wlan", IsWlan: true}}
	}}
	defer stub.install()()

	devs := waitProbed(t, 5*time.Second)
	if len(devs) != 1 || devs[0].Name != "wlan" {
		t.Fatalf("warm cache = %v", devs)
	}
	before := stub.callCount()
	for i := 0; i < 10; i++ {
		if _, probed := CachedDongles(); !probed {
			t.Fatal("warm cache stopped reporting probed")
		}
	}
	if got := stub.callCount(); got != before {
		t.Fatalf("warm reads re-enumerated: %d passes, was %d", got, before)
	}
}

// A timed-out refresh can finish after its replacement has already observed an
// unplug. Its old device list must not resurrect the removed dongle.
func TestCachedDonglesLatePassCannotOverwriteNewerResult(t *testing.T) {
	resetDongleCache()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	stub := &stubEnumerate{}
	calls := 0
	stub.fn = func() []DeviceInfo {
		stub.mu.Lock()
		calls++
		call := calls
		stub.mu.Unlock()
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
			close(firstDone)
			return []DeviceInfo{{Name: "removed dongle", IsWlan: true}}
		}
		return nil
	}
	defer stub.install()()
	CachedDongles()
	<-firstStarted
	dongleCacheMu.Lock()
	dongleCacheRefreshAt = time.Now().Add(-2 * dongleCacheForceAfter)
	dongleCacheMu.Unlock()
	if got := waitProbed(t, time.Second); len(got) != 0 {
		t.Fatalf("replacement = %v", got)
	}
	close(releaseFirst)
	<-firstDone
	// Let completion publish; the assertion checks the final completed state.
	time.Sleep(20 * time.Millisecond)
	if got, _ := CachedDongles(); len(got) != 0 {
		t.Fatalf("late pass resurrected unplugged device: %v", got)
	}
}
