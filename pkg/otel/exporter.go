package otel

import (
	"fmt"
	"strings"

	"github.com/castlemilk/event-horizon/pkg/netstat"
	"github.com/castlemilk/event-horizon/pkg/uptime"
)

type OTelExporter struct{}

func NewExporter() *OTelExporter {
	return &OTelExporter{}
}

// ExportPrometheusMetrics renders OpenTelemetry / Prometheus plain-text metrics
func (e *OTelExporter) ExportPrometheusMetrics(stats []netstat.InterfaceStat, stability uptime.StabilityStats) string {
	var sb strings.Builder

	sb.WriteString("# HELP net_bytes_rx_total Total received bytes per network interface\n")
	sb.WriteString("# TYPE net_bytes_rx_total counter\n")
	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("net_bytes_rx_total{interface=\"%s\"} %d\n", s.Name, s.BytesIn))
	}

	sb.WriteString("# HELP net_bytes_tx_total Total transmitted bytes per network interface\n")
	sb.WriteString("# TYPE net_bytes_tx_total counter\n")
	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("net_bytes_tx_total{interface=\"%s\"} %d\n", s.Name, s.BytesOut))
	}

	sb.WriteString("# HELP net_packets_rx_total Total received packets per interface\n")
	sb.WriteString("# TYPE net_packets_rx_total counter\n")
	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("net_packets_rx_total{interface=\"%s\"} %d\n", s.Name, s.PacketsIn))
	}

	sb.WriteString("# HELP net_packets_tx_total Total transmitted packets per interface\n")
	sb.WriteString("# TYPE net_packets_tx_total counter\n")
	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("net_packets_tx_total{interface=\"%s\"} %d\n", s.Name, s.PacketsOut))
	}

	sb.WriteString("# HELP net_bandwidth_rx_kbps Current download speed in KB/s\n")
	sb.WriteString("# TYPE net_bandwidth_rx_kbps gauge\n")
	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("net_bandwidth_rx_kbps{interface=\"%s\"} %.2f\n", s.Name, s.RxRateKBps))
	}

	sb.WriteString("# HELP net_bandwidth_tx_kbps Current upload speed in KB/s\n")
	sb.WriteString("# TYPE net_bandwidth_tx_kbps gauge\n")
	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("net_bandwidth_tx_kbps{interface=\"%s\"} %.2f\n", s.Name, s.TxRateKBps))
	}

	sb.WriteString("# HELP net_link_uptime_seconds Link connection duration in seconds\n")
	sb.WriteString("# TYPE net_link_uptime_seconds gauge\n")
	sb.WriteString(fmt.Sprintf("net_link_uptime_seconds %d\n", stability.UptimeSeconds))

	sb.WriteString("# HELP net_link_stability_percent Link stability score percentage\n")
	sb.WriteString("# TYPE net_link_stability_percent gauge\n")
	sb.WriteString(fmt.Sprintf("net_link_stability_percent %.2f\n", stability.StabilityScore))

	return sb.String()
}
