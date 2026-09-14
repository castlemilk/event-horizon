package netstat

import (
	"testing"
	"time"
)

func TestMonitorHotplugDoesNotReuseMissingInterfaceBaseline(t *testing.T) {
	monitor := NewMonitor()
	now := time.Now()
	sample := []InterfaceStat{{Name: "en9", IsUp: true, BytesIn: 1024, BytesOut: 1024}}
	monitor.readStats = func() []InterfaceStat { return sample }
	monitor.now = func() time.Time { return now }
	monitor.GetInterfaceStats()
	now = now.Add(time.Second)
	sample = []InterfaceStat{}
	monitor.GetInterfaceStats()
	now = now.Add(time.Second)
	sample = []InterfaceStat{{Name: "en9", IsUp: true, BytesIn: 1048576, BytesOut: 1048576}}
	got := monitor.GetInterfaceStats()[0]
	if got.RxRateKBps != 0 || got.TxRateKBps != 0 {
		t.Fatalf("new interface inherited old rate: %+v", got)
	}
}

func TestMonitorDoesNotReportTrafficWhileInterfaceIsDown(t *testing.T) {
	monitor := NewMonitor()
	now := time.Now()
	sample := []InterfaceStat{{Name: "en9", IsUp: true, BytesIn: 1024, BytesOut: 1024}}
	monitor.readStats = func() []InterfaceStat { return sample }
	monitor.now = func() time.Time { return now }
	monitor.GetInterfaceStats()
	now = now.Add(time.Second)
	sample = []InterfaceStat{{Name: "en9", IsUp: false, BytesIn: 2048, BytesOut: 2048}}
	got := monitor.GetInterfaceStats()[0]
	if got.RxRateKBps != 0 || got.TxRateKBps != 0 {
		t.Fatalf("down interface reports traffic: %+v", got)
	}
	now = now.Add(time.Second)
	sample[0].IsUp = true
	sample[0].BytesIn = 4096
	got = monitor.GetInterfaceStats()[0]
	if got.RxRateKBps != 0 {
		t.Fatalf("reconnected interface reused down baseline: %+v", got)
	}
}

func TestMonitorCounterResetAndRates(t *testing.T) {
	monitor := NewMonitor()
	now := time.Now()
	sample := []InterfaceStat{{Name: "utun11", IsUp: true, BytesIn: 8192, BytesOut: 4096}}
	monitor.readStats = func() []InterfaceStat { return sample }
	monitor.now = func() time.Time { return now }
	monitor.GetInterfaceStats()
	now = now.Add(2 * time.Second)
	sample[0].BytesIn = 12288
	sample[0].BytesOut = 6144
	if got := monitor.GetInterfaceStats()[0]; got.RxRateKBps != 2 || got.TxRateKBps != 1 {
		t.Fatalf("rates = %+v", got)
	}
	now = now.Add(time.Second)
	sample[0].BytesIn = 128
	sample[0].BytesOut = 64
	if got := monitor.GetInterfaceStats()[0]; got.RxRateKBps != 0 || got.TxRateKBps != 0 {
		t.Fatalf("counter reset spike = %+v", got)
	}
}
