package routing

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/castlemilk/event-horizon/pkg/ping"
)

type InterfaceRouteInfo struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	Gateway     string `json:"gateway"`
	IsDefault   bool   `json:"is_default"`
	Metric      int    `json:"metric"`
	IsReachable bool   `json:"is_reachable"`
	Description string `json:"description"`
}

type FailoverEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	FromIface   string    `json:"from_iface"`
	ToIface     string    `json:"to_iface"`
	Reason      string    `json:"reason"`
}

type RoutingPolicyReport struct {
	ActiveDefaultInterface string               `json:"active_default_interface"`
	AutoFailoverEnabled    bool                 `json:"auto_failover_enabled"`
	Interfaces             []InterfaceRouteInfo `json:"interfaces"`
	RecentEvents           []FailoverEvent      `json:"recent_events"`
	LastEvaluated          time.Time            `json:"last_evaluated"`
}

type PolicyManager struct {
	mu                  sync.RWMutex
	autoFailover        atomic.Bool
	recentEvents        []FailoverEvent
	tester              *ping.Tester
	stopCh              chan struct{}
	running             atomic.Bool
	consecutiveFailures int
}

var (
	globalManager *PolicyManager
	managerOnce   sync.Once
)

func GetPolicyManager() *PolicyManager {
	managerOnce.Do(func() {
		globalManager = &PolicyManager{
			tester:       ping.NewTester(),
			stopCh:       make(chan struct{}),
			recentEvents: make([]FailoverEvent, 0),
		}
		globalManager.autoFailover.Store(true)
		globalManager.StartWatchdog()
	})
	return globalManager
}

func (m *PolicyManager) StartWatchdog() {
	if m.running.CompareAndSwap(false, true) {
		go m.watchdogLoop()
	}
}

func (m *PolicyManager) StopWatchdog() {
	if m.running.CompareAndSwap(true, false) {
		close(m.stopCh)
	}
}

func (m *PolicyManager) SetAutoFailover(enabled bool) {
	m.autoFailover.Store(enabled)
	log.Printf("[ROUTING] Auto-failover set to: %v", enabled)
}

func (m *PolicyManager) IsAutoFailoverEnabled() bool {
	return m.autoFailover.Load()
}

func (m *PolicyManager) GetReport() RoutingPolicyReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	defaultIface := GetActiveDefaultInterface()
	routes := DiscoverInterfaceRoutes()

	// Update reachability status
	for i := range routes {
		if routes[i].Name == defaultIface {
			routes[i].IsDefault = true
		}
		if routes[i].Gateway != "" {
			res := m.tester.PingTargetOnInterface(routes[i].Name, routes[i].Gateway, 53)
			routes[i].IsReachable = res.IsReachable
		}
	}

	events := make([]FailoverEvent, len(m.recentEvents))
	copy(events, m.recentEvents)

	return RoutingPolicyReport{
		ActiveDefaultInterface: defaultIface,
		AutoFailoverEnabled:    m.autoFailover.Load(),
		Interfaces:             routes,
		RecentEvents:           events,
		LastEvaluated:          time.Now(),
	}
}

func (m *PolicyManager) SetDefaultInterface(targetIface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	routes := DiscoverInterfaceRoutes()
	var targetRoute *InterfaceRouteInfo
	for i := range routes {
		if routes[i].Name == targetIface {
			targetRoute = &routes[i]
			break
		}
	}

	if targetRoute == nil {
		return fmt.Errorf("interface %s not found or has no active IPv4 configuration", targetIface)
	}

	currentDefault := GetActiveDefaultInterface()
	if currentDefault == targetIface {
		return nil // Already default
	}

	// Change default route on macOS:
	// route change default -interface <iface> or route change default <gateway>
	var cmd *exec.Cmd
	if targetRoute.Gateway != "" {
		cmd = exec.Command("route", "change", "default", targetRoute.Gateway)
	} else {
		cmd = exec.Command("route", "change", "default", "-interface", targetIface)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: try delete and add
		exec.Command("route", "delete", "default").Run()
		if targetRoute.Gateway != "" {
			cmd = exec.Command("route", "add", "default", targetRoute.Gateway)
		} else {
			cmd = exec.Command("route", "add", "default", "-interface", targetIface)
		}
		out, err = cmd.CombinedOutput()
	}

	if err != nil {
		log.Printf("[ROUTING] Failed to change default route to %s: %v (output: %s)", targetIface, err, strings.TrimSpace(string(out)))
		return fmt.Errorf("route change failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	event := FailoverEvent{
		Timestamp: time.Now(),
		FromIface: currentDefault,
		ToIface:   targetIface,
		Reason:    "Manual user route override",
	}
	m.recentEvents = append(m.recentEvents, event)
	if len(m.recentEvents) > 20 {
		m.recentEvents = m.recentEvents[1:]
	}

	log.Printf("[ROUTING] ✅ Active default route changed to %s (was %s)", targetIface, currentDefault)
	return nil
}

func (m *PolicyManager) watchdogLoop() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			if !m.autoFailover.Load() {
				continue
			}

			defaultIface := GetActiveDefaultInterface()
			if defaultIface == "" {
				continue
			}

			// Probe default interface health
			routes := DiscoverInterfaceRoutes()
			var currentRoute *InterfaceRouteInfo
			var alternateRoutes []*InterfaceRouteInfo

			for i := range routes {
				if routes[i].Name == defaultIface {
					currentRoute = &routes[i]
				} else if routes[i].Name == "en0" || strings.HasPrefix(routes[i].Name, "utun") || strings.HasPrefix(routes[i].Name, "en") {
					alternateRoutes = append(alternateRoutes, &routes[i])
				}
			}

			if currentRoute == nil || currentRoute.Gateway == "" {
				continue
			}

			// Ping gateway
			res := m.tester.PingTargetOnInterface(currentRoute.Name, currentRoute.Gateway, 53)
			if !res.IsReachable {
				m.consecutiveFailures++
				log.Printf("[ROUTING-WATCHDOG] Gateway %s unreachable on %s (strike %d/3)", currentRoute.Gateway, currentRoute.Name, m.consecutiveFailures)

				if m.consecutiveFailures >= 3 && len(alternateRoutes) > 0 {
					// Find healthy alternate
					for _, alt := range alternateRoutes {
						if alt.Gateway != "" {
							altRes := m.tester.PingTargetOnInterface(alt.Name, alt.Gateway, 53)
							if altRes.IsReachable {
								log.Printf("[ROUTING-WATCHDOG] 🚨 Triggering automatic failover from %s to %s!", currentRoute.Name, alt.Name)
								_ = m.SetDefaultInterface(alt.Name)
								m.consecutiveFailures = 0

								m.mu.Lock()
								m.recentEvents = append(m.recentEvents, FailoverEvent{
									Timestamp: time.Now(),
									FromIface: currentRoute.Name,
									ToIface:   alt.Name,
									Reason:    fmt.Sprintf("Default link failure (%s unreachable)", currentRoute.Gateway),
								})
								m.mu.Unlock()
								break
							}
						}
					}
				}
			} else {
				if m.consecutiveFailures > 0 {
					m.consecutiveFailures = 0
				}
			}
		}
	}
}

func GetActiveDefaultInterface() string {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return "en0"
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	return "en0"
}

func DiscoverInterfaceRoutes() []InterfaceRouteInfo {
	var result []InterfaceRouteInfo

	ifaces, err := net.Interfaces()
	if err != nil {
		return result
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		var ipv4 string
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				ipv4 = ipnet.IP.String()
				break
			}
		}

		if ipv4 == "" {
			continue
		}

		gateway := ""
		desc := "Standard Network Adapter"
		if iface.Name == "en0" {
			desc = "Apple Built-in Wi-Fi"
			gateway = getInterfaceGateway("en0")
		} else if strings.HasPrefix(iface.Name, "utun") {
			desc = "Event Horizon USB Wi-Fi / Starlink Tunnel"
			gateway = "192.168.100.1"
		} else if strings.HasPrefix(iface.Name, "en") {
			desc = "Ethernet / USB Adapter"
			gateway = getInterfaceGateway(iface.Name)
		}

		result = append(result, InterfaceRouteInfo{
			Name:        iface.Name,
			IP:          ipv4,
			Gateway:     gateway,
			Metric:      10,
			Description: desc,
		})
	}

	return result
}

func getInterfaceGateway(ifaceName string) string {
	out, err := exec.Command("ipconfig", "getsummary", ifaceName).Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Router :") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Router :"))
			}
		}
	}
	return "192.168.0.1"
}
