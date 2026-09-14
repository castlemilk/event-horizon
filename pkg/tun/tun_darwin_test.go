package tun

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// stubRunCmd replaces runCmdFn with a scripted responder: map exact
// command string -> output lines or error. Unmatched commands fail the
// test loudly rather than touching the real routing table — a route
// test that mutates the host table is a test that breaks the operator's
// networking.
func stubRunCmd(t *testing.T, script map[string]struct {
	out string
	err error
}) {
	t.Helper()
	old := runCmdFn
	runCmdFn = func(name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if s, ok := script[key]; ok {
			return []byte(s.out), s.err
		}
		t.Fatalf("unexpected command: %q", key)
		return nil, errors.New("unexpected")
	}
	t.Cleanup(func() { runCmdFn = old })
}

const netstatTwo = `Name  Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll
utun11 1500 <Link#48> 100 0 1000 50 0 500 0
en0 1500 <Link#6> 999 0 9999 888 0 8888 0
`

func TestEnsureHostRouteAddsWhenAbsent(t *testing.T) {
	stubRunCmd(t, map[string]struct {
		out string
		err error
	}{
		"route -n get 192.168.100.1": {err: errors.New("not in table")},
		"route -n add -host 192.168.100.1 -interface utun12": {},
	})
	iface := &Interface{Name: "utun12"}
	if err := iface.EnsureHostRoute("192.168.100.1", 0); err != nil {
		t.Fatalf("add on empty table: %v", err)
	}
}

func TestEnsureHostRouteNoopWhenAlreadyOurs(t *testing.T) {
	stubRunCmd(t, map[string]struct {
		out string
		err error
	}{
		"route -n get 192.168.100.1": {out: "route to: 192.168.100.1\ninterface: utun12\n"},
	})
	iface := &Interface{Name: "utun12"}
	if err := iface.EnsureHostRoute("192.168.100.1", 0); err != nil {
		t.Fatalf("already-ours should no-op: %v", err)
	}
}

func TestEnsureHostRouteRefusesNonUtun(t *testing.T) {
	stubRunCmd(t, map[string]struct {
		out string
		err error
	}{
		"route -n get 192.168.100.1": {out: "route to: 192.168.100.1\ninterface: en0\n"},
	})
	iface := &Interface{Name: "utun12"}
	err := iface.EnsureHostRoute("192.168.100.1", 0)
	if err == nil || !strings.Contains(err.Error(), "non-utun") {
		t.Fatalf("must refuse to steal from en0, got: %v", err)
	}
}

func TestEnsureHostRouteReplacesIdleUtun(t *testing.T) {
	stubRunCmd(t, map[string]struct {
		out string
		err error
	}{
		"route -n get 192.168.100.1":                {out: "route to: 192.168.100.1\ninterface: utun11\n"},
		"netstat -ibn":                              {out: netstatTwo},
		"route -n delete -host 192.168.100.1":       {},
		"route -n add -host 192.168.100.1 -interface utun12": {},
	})
	iface := &Interface{Name: "utun12"}
	// Zero wait: single-sample idle counts as idle (production passes 3s).
	if err := iface.EnsureHostRoute("192.168.100.1", 0); err != nil {
		t.Fatalf("idle utun should be replaced: %v", err)
	}
}

func TestRouteIfaceForParses(t *testing.T) {
	stubRunCmd(t, map[string]struct {
		out string
		err error
	}{
		"route -n get 1.2.3.4": {out: "route to: 1.2.3.4\nxxx\ninterface: utun9\n"},
	})
	got, err := routeIfaceFor("1.2.3.4")
	if err != nil || got != "utun9" {
		t.Fatalf("iface = %q, err = %v", got, err)
	}
}

func TestUtunIdleDetectsTraffic(t *testing.T) {
	calls := 0
	old := runCmdFn
	runCmdFn = func(name string, args ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte(netstatTwo), nil
		}
		// Second sample moved: not idle.
		return []byte(strings.Replace(netstatTwo, " 100 0 1000 50 0 500 0", " 200 0 2000 60 0 600 0", 1)), nil
	}
	t.Cleanup(func() { runCmdFn = old })
	idle, err := utunIdle("utun11", time.Millisecond)
	if err != nil {
		t.Fatalf("counters: %v", err)
	}
	if idle {
		t.Error("moved counters must not read idle")
	}
}
