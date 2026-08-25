package supervisor

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"sync"
	"time"
)

// EventSeverity categorizes supervisor event log entries.
type EventSeverity string

const (
	SeverityInfo    EventSeverity = "info"
	SeveritySuccess EventSeverity = "success"
	SeverityWarning EventSeverity = "warning"
	SeverityError   EventSeverity = "error"
)

// SupervisorEvent records an autonomous lifecycle, self-healing, or hardware event.
type SupervisorEvent struct {
	ID        int64         `json:"id"`
	Timestamp string        `json:"timestamp"`
	Severity  EventSeverity `json:"severity"`
	Component string        `json:"component"`
	Message   string        `json:"message"`
	Details   string        `json:"details,omitempty"`
}

// DeviceCheckFunc is a pluggable callback to check for hardware presence.
type DeviceCheckFunc func() (hasDevice bool, name string, vid, pid uint16)

// Watchdog tracks USB hardware connectivity, virtual network interfaces, and self-healing.
type Watchdog struct {
	mu             sync.RWMutex
	running        bool
	stopChan       chan struct{}
	events         []SupervisorEvent
	nextEventID    int64
	lastHealthTime time.Time
	isHardwareUp   bool
	isTunUp        bool
	isGatewayUp    bool
	healCount      int
	deviceChecker  DeviceCheckFunc
}

var globalWatchdog *Watchdog
var once sync.Once

// GetWatchdog returns the global singleton watchdog instance.
func GetWatchdog() *Watchdog {
	once.Do(func() {
		globalWatchdog = &Watchdog{
			events:         make([]SupervisorEvent, 0),
			stopChan:       make(chan struct{}),
			lastHealthTime: time.Now(),
		}
		globalWatchdog.LogEvent(SeverityInfo, "WATCHDOG", "Autonomous Runtime Supervisor initialized", "Supervising USB bus, utun virtual interface, and routing health")
	})
	return globalWatchdog
}

// SetDeviceChecker configures the hardware probe callback.
func (w *Watchdog) SetDeviceChecker(checker DeviceCheckFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deviceChecker = checker
}

// Start launches the background monitoring and self-healing loop.
func (w *Watchdog) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go w.supervisionLoop()
}

// Stop terminates the watchdog loop cleanly.
func (w *Watchdog) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	close(w.stopChan)
}

func (w *Watchdog) supervisionLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.runHealthChecks()
		}
	}
}

// runHealthChecks performs holistic diagnostics across USB hardware and network routing.
func (w *Watchdog) runHealthChecks() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lastHealthTime = time.Now()

	// 1. Check attached USB dongles if checker configured
	if w.deviceChecker != nil {
		hardwarePresent, name, vid, pid := w.deviceChecker()
		if hardwarePresent != w.isHardwareUp {
			w.isHardwareUp = hardwarePresent
			if hardwarePresent {
				w.addEventLocked(SeveritySuccess, "USB-BUS", fmt.Sprintf("Hardware dongle detected: %s (VID %04x PID %04x)", name, vid, pid), "")
			} else {
				w.addEventLocked(SeverityWarning, "USB-BUS", "No USB Wi-Fi dongle attached to bus", "Awaiting device insertion")
			}
		}
	}

	// 2. Check utun10 interface status
	tunInterface, err := net.InterfaceByName("utun10")
	tunPresent := err == nil && (tunInterface.Flags&net.FlagUp != 0)

	if tunPresent != w.isTunUp {
		w.isTunUp = tunPresent
		if tunPresent {
			w.addEventLocked(SeveritySuccess, "TUN-ENGINE", "Virtual network interface 'utun10' active and UP", fmt.Sprintf("MTU: %d, Flags: %s", tunInterface.MTU, tunInterface.Flags))
		} else {
			w.addEventLocked(SeverityWarning, "TUN-ENGINE", "Interface 'utun10' is DOWN or missing", "Attempting autonomous self-healing...")
			w.healTunInterfaceLocked()
		}
	}
}

// healTunInterfaceLocked executes self-healing recovery commands.
func (w *Watchdog) healTunInterfaceLocked() {
	w.healCount++
	log.Printf("[SUPERVISOR-HEAL] Re-configuring utun10 interface with IP 192.168.100.2...")

	out, err := exec.Command("ifconfig", "utun10", "192.168.100.2", "192.168.100.1", "up").CombinedOutput()
	if err != nil {
		w.addEventLocked(SeverityError, "SELF-HEAL", "Failed to restore utun10 interface", string(out))
	} else {
		w.addEventLocked(SeveritySuccess, "SELF-HEAL", "Successfully healed and restored utun10 interface", "Assigned 192.168.100.2 -> 192.168.100.1")
		// Add static route
		_ = exec.Command("route", "add", "-host", "192.168.100.1", "-interface", "utun10").Run()
	}
}

// LogEvent adds a structured log event to the supervisor audit history.
func (w *Watchdog) LogEvent(severity EventSeverity, component, message, details string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.addEventLocked(severity, component, message, details)
}

func (w *Watchdog) addEventLocked(severity EventSeverity, component, message, details string) {
	w.nextEventID++
	evt := SupervisorEvent{
		ID:        w.nextEventID,
		Timestamp: time.Now().Format(time.RFC3339),
		Severity:  severity,
		Component: component,
		Message:   message,
		Details:   details,
	}
	w.events = append(w.events, evt)
	if len(w.events) > 100 {
		w.events = w.events[len(w.events)-100:]
	}
	log.Printf("[%s] %s: %s (%s)", component, severity, message, details)
}

// GetStatus returns the current supervisor health state and recent event log.
func (w *Watchdog) GetStatus() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return map[string]interface{}{
		"is_running":       w.running,
		"last_health_time": w.lastHealthTime.Format(time.RFC3339),
		"hardware_up":      w.isHardwareUp,
		"tun_up":           w.isTunUp,
		"heal_count":       w.healCount,
		"events":           w.events,
	}
}
