package usb

import "testing"

func TestBridgeInterfacesRejectsUnrelatedTunnelNames(t *testing.T) {
    if got := BridgeInterfaces(true, "bridged on utunicorn as 192.0.2.4", "Office"); len(got) != 0 {
        t.Fatalf("non-interface word accepted as tunnel: %+v", got)
    }
}

func TestBridgeInterfacesMissingBridgeIsNotActive(t *testing.T) {
    got := BridgeInterfaces(true, "bridged on utun999999 as 192.0.2.4", "Office")
    if len(got) != 1 || got[0].IPAddress != "" || got[0].Status == "Active (user-space bridge)" { t.Fatalf("missing bridge active: %+v", got) }
    if got[0].NetworkTarget != "" { t.Fatalf("missing bridge retains association: %+v", got) }
}

func TestMergeBridgeTopologyMapsSingleRadioAndDeduplicates(t *testing.T) {
    physical := HardwareTopology{VendorID:"0xa69c", ProductID:"0x8d80", SerialNumber:"radio", BusPath:"1-2", USBDriver:"UGREEN AX900", Status:"Present"}
    bridge := HardwareTopology{BSDInterface:"utun11", IPAddress:"192.0.2.4", NetworkTarget:"Office", Status:"Active (user-space bridge)", DriverType:"User-space libusb + utun"}
    got := MergeBridgeTopology([]HardwareTopology{physical}, []HardwareTopology{bridge})
    if len(got) != 1 || got[0].SerialNumber != "radio" || got[0].BSDInterface != "utun11" || got[0].NetworkTarget != "Office" { t.Fatalf("unmapped bridge: %+v", got) }
    got = MergeBridgeTopology(got, []HardwareTopology{bridge})
    if len(got) != 1 || got[0].SerialNumber != "radio" { t.Fatalf("duplicate bridge: %+v", got) }
}

func TestMergeBridgeTopologyDoesNotGuessAmongRadios(t *testing.T) {
    first := HardwareTopology{VendorID:"0xa69c", ProductID:"0x8d80", SerialNumber:"first", Status:"Present"}
    second := first
    second.SerialNumber = "second"
    bridge := HardwareTopology{BSDInterface:"utun11", IPAddress:"192.0.2.4", Status:"Active (user-space bridge)"}
    got := MergeBridgeTopology([]HardwareTopology{first, second}, []HardwareTopology{bridge})
    if len(got) != 3 || got[0].BSDInterface != "" || got[1].BSDInterface != "" { t.Fatalf("guessed hardware ownership: %+v", got) }
}
