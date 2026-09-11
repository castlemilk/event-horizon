package usb

import (
	"net"
	"strings"
)

// The dongle disappears when it starts working, and that needs saying.
//
// A user replugs the dongle and watches it appear in the device list — it is in
// ZeroCD storage mode, so macOS enumerates it normally. Then the link comes up,
// libusb claims the device exclusively, and it vanishes: gone from ioreg, gone
// from the hardware topology, gone from the UI. It looks broken at precisely
// the moment it starts working.
//
// Nothing was reporting the other half either. The bridge the dongle is
// carrying traffic over is a utun interface, and the topology only ever
// enumerated hardware ports, so utun11 — holding the address, the route and all
// of the traffic — appeared nowhere.
//
// So a claimed dongle is reported as claimed, and its bridge is listed with it.

// BridgeInterfaces returns the utun bridges this daemon is running, as topology
// entries, so a working link is visible rather than merely absent.
//
// linkDetail is the link service's own description ("bridged on utun11 as
// 192.168.2.243"); the interface name is taken from it so this reports the
// bridge the link actually built rather than every utun on the machine — VPNs
// and other tunnels are not ours to claim.
func BridgeInterfaces(linkUp bool, linkDetail, ssid string) []HardwareTopology {
	if !linkUp {
		return nil
	}
	name := utunFromDetail(linkDetail)
	if name == "" {
		return nil
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		// The link claims a bridge that does not exist. Report it as such
		// rather than silently omitting it: a link reporting "up" over a
		// missing interface is exactly the inconsistency worth surfacing.
		return []HardwareTopology{{
			USBDriver:     "USB Wi-Fi dongle (bridge missing)",
			BSDInterface:  name,
			NetworkTarget: ssid,
			Status:        "Link reports up, but " + name + " does not exist",
			DriverType:    "User-space libusb + utun",
		}}
	}

	addr, mask := firstIPv4(iface)
	return []HardwareTopology{{
		USBDriver:     "USB Wi-Fi dongle (claimed by this daemon)",
		BSDInterface:  name,
		NetworkTarget: ssid,
		IPAddress:     addr,
		SubnetMask:    mask,
		MACAddress:    iface.HardwareAddr.String(),
		Status:        "Active (user-space bridge)",
		DriverType:    "User-space libusb + utun",
	}}
}

// ClaimedDongleNote describes a dongle that is in use and therefore no longer
// enumerable, so the UI can say why it disappeared instead of showing nothing.
func ClaimedDongleNote(linkUp bool) string {
	if !linkUp {
		return ""
	}
	return "The dongle is held exclusively by the user-space driver while the link is up, " +
		"so it no longer appears on the USB bus. That is expected: it is in use, not missing."
}

// utunFromDetail pulls the interface name out of the link service's detail
// string. Matching on "utun" keeps this to the bridges this daemon builds.
func utunFromDetail(detail string) string {
	for _, f := range strings.Fields(detail) {
		f = strings.Trim(f, ",;:")
		if strings.HasPrefix(f, "utun") {
			return f
		}
	}
	return ""
}

func firstIPv4(iface *net.Interface) (addr, mask string) {
	addrs, err := iface.Addrs()
	if err != nil {
		return "", ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			m := ipn.Mask
			return ipn.IP.String(), net.IP(m).String()
		}
	}
	return "", ""
}
