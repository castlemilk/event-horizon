package usb

import "fmt"

// hardwareAliases maps USB vendor/product IDs to friendly, human-readable
// names. VID/PID are read from the real USB descriptor, never fabricated.
// Keys are lowercase "vid:pid" hex. Prefer the alias whenever a device is
// matched so the UI shows a nice name ("UGREEN AX900 …") instead of the raw
// chipset string the firmware reports ("AIC Wlan").
var hardwareAliases = map[string]string{
	// AICSEMI AIC8800D80 — the chipset behind UGREEN AX900 / CM762 /
	// WIFI6-BW22 / 88M80 WiFi 6 USB adapters. Three-stage boot:
	//   1111:1111 fake CD-ROM (ZeroCD) -> a69c:8d80 boot ROM -> operational.
	"1111:1111": "AIC8800 ZeroCD (fake CD-ROM)",
	"a69c:8d80": "UGREEN AX900 WiFi 6 (AIC8800D80 boot ROM)",
	"a69c:8d81": "UGREEN AX900 WiFi 6 (AIC8800D80 WiFi+BT)",
	"a69c:8d83": "UGREEN AX900 WiFi 6 (AIC8800D80 WiFi)",
	"a69c:5721": "AIC8800 WiFi 6 (ZeroCD storage mode)",
	"a69c:5723": "AIC8800 WiFi 6 (ZeroCD storage mode)",
	"a69c:5724": "AIC8800 WiFi 6 (ZeroCD storage mode)",

	// Realtek RTL881x — single/dual-band 802.11ac USB dongles.
	"0bda:8811": "Realtek RTL8811AU 802.11ac dongle",
	"0bda:8812": "Realtek RTL8812AU 802.11ac dongle",
	"0bda:c820": "Realtek RTL8821AU 802.11ac dongle",

	// Realtek RTL8188-family 802.11n USB dongles.
	"0bda:8179": "Realtek RTL8188EU 802.11n dongle",
	"0bda:8176": "Realtek RTL8188CTV 802.11n dongle",
	"0bda:8188": "Realtek RTL8188FTV 802.11n dongle",
	"0bda:8171": "Realtek RTL8188SU 802.11n dongle",
	"0bda:1a2b": "Realtek RTL8188GU 802.11n dongle",

	// Realtek RTL8723BU / RTL8821CU 802.11ac USB dongles.
	"0bda:b720": "Realtek RTL8723BU 802.11ac dongle",
	"0bda:b723": "Realtek RTL8723BU 802.11ac dongle",

	// Realtek USB LAN adapters — ethernet, NOT Wi-Fi. Listed so aliases render
	// correctly if they ever appear in the UI, but classification rejects them.
	"0bda:8156": "Realtek RTL8156 2.5GbE USB adapter",
	"0bda:8153": "Realtek RTL8153 Gigabit USB adapter",
	"0bda:8152": "Realtek RTL8152 Fast Ethernet USB adapter",
	"0bda:1100": "Realtek RTL8152 Fast Ethernet USB adapter",
	"0bda:1101": "Realtek RTL8152 Fast Ethernet USB adapter",

	// JMicron USB-SATA bridges used inside Ugreen external drive enclosures.
	"174c:2463": "Ugreen external drive enclosure (JMicron JMS578)",
	"174c:55aa": "JMicron JMS578 USB-SATA bridge",
	"174c:3074": "JMicron JMS578 USB-SATA bridge",
	"174c:1351": "JMicron JMS1351 USB-SATA bridge",
}

// AliasFor returns a friendly hardware name for a VID/PID. The fallback is the
// raw USB product descriptor string, or a caller-supplied generic label.
func AliasFor(vid, pid uint16, fallback string) string {
	key := fmt.Sprintf("%04x:%04x", vid, pid)
	if name, ok := hardwareAliases[key]; ok {
		return name
	}
	return fallback
}
