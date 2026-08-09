package otel

import (
	"strings"
	"testing"

	"github.com/castlemilk/event-horizon/pkg/netstat"
	"github.com/castlemilk/event-horizon/pkg/uptime"
)

func TestPrometheusExporter(t *testing.T) {
	exporter := NewExporter()
	stats := []netstat.InterfaceStat{
		{
			Name:       "en0",
			BytesIn:    1024,
			BytesOut:   2048,
			PacketsIn:  10,
			PacketsOut: 20,
			IsUp:       true,
			RxRateKBps: 512.0,
			TxRateKBps: 128.0,
		},
	}
	stability := uptime.StabilityStats{
		UptimeSeconds:  3600,
		StabilityScore: 99.5,
	}

	metricsText := exporter.ExportPrometheusMetrics(stats, stability)

	if !strings.Contains(metricsText, "net_bytes_rx_total{interface=\"en0\"} 1024") {
		t.Errorf("Prometheus output missing rx_total metric for en0")
	}

	if !strings.Contains(metricsText, "net_link_stability_percent 99.50") {
		t.Errorf("Prometheus output missing link stability metric")
	}
}
