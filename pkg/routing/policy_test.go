package routing

import (
	"testing"
)

func TestDiscoverInterfaceRoutes(t *testing.T) {
	routes := DiscoverInterfaceRoutes()
	if len(routes) == 0 {
		t.Logf("No active network routes discovered in test environment")
		return
	}

	foundLoopback := false
	for _, r := range routes {
		if r.Name == "lo0" {
			foundLoopback = true
		}
		if r.IP == "" {
			t.Errorf("Interface %s has empty IP address", r.Name)
		}
	}

	if foundLoopback {
		t.Errorf("Loopback interface should be excluded from route discovery")
	}
}

func TestGetActiveDefaultInterface(t *testing.T) {
	def := GetActiveDefaultInterface()
	if def == "" {
		t.Errorf("Expected non-empty default interface")
	}
}

func TestPolicyManagerReport(t *testing.T) {
	mgr := GetPolicyManager()
	rep := mgr.GetReport()

	if rep.ActiveDefaultInterface == "" {
		t.Errorf("Expected non-empty active default interface in report")
	}

	mgr.SetAutoFailover(true)
	if !mgr.IsAutoFailoverEnabled() {
		t.Errorf("Expected auto-failover to be enabled")
	}
}
