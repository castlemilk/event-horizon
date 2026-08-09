package uptime

import (
	"fmt"
	"sync"
	"time"
)

type StabilityStats struct {
	ConnectedAt        time.Time `json:"connected_at"`
	UptimeSeconds      int64     `json:"uptime_seconds"`
	UptimeFormatted    string    `json:"uptime_formatted"`
	DisconnectCount    int       `json:"disconnect_count"`
	ReconnectCount     int       `json:"reconnect_count"`
	StabilityScore     float64   `json:"stability_score_percent"`
	LastStateChange    time.Time `json:"last_state_change"`
	CurrentStatus      string    `json:"current_status"`
}

type Tracker struct {
	mu              sync.RWMutex
	connectedAt     time.Time
	disconnectCount int
	reconnectCount  int
	currentStatus   string
	lastStateChange time.Time
}

func NewTracker() *Tracker {
	now := time.Now()
	return &Tracker{
		connectedAt:     now,
		currentStatus:   "CONNECTED",
		lastStateChange: now,
	}
}

func (t *Tracker) RecordDisconnect() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.disconnectCount++
	t.currentStatus = "DISCONNECTED"
	t.lastStateChange = time.Now()
}

func (t *Tracker) RecordReconnect() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.reconnectCount++
	t.currentStatus = "CONNECTED"
	t.connectedAt = time.Now()
	t.lastStateChange = time.Now()
}

func (t *Tracker) GetStats() StabilityStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	uptimeSec := int64(time.Since(t.connectedAt).Seconds())
	if t.currentStatus != "CONNECTED" {
		uptimeSec = 0
	}

	hours := uptimeSec / 3600
	minutes := (uptimeSec % 3600) / 60
	seconds := uptimeSec % 60
	formatted := fmt.Sprintf("%02dh %02dm %02ds", hours, minutes, seconds)

	score := 100.0 - float64(t.disconnectCount*5)
	if score < 0 {
		score = 0
	}

	return StabilityStats{
		ConnectedAt:        t.connectedAt,
		UptimeSeconds:      uptimeSec,
		UptimeFormatted:    formatted,
		DisconnectCount:    t.disconnectCount,
		ReconnectCount:     t.reconnectCount,
		StabilityScore:     score,
		LastStateChange:    t.lastStateChange,
		CurrentStatus:      t.currentStatus,
	}
}
