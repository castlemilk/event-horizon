import {
  Activity,
  Boxes,
  Cpu,
  Gauge,
  KeyRound,
  Radio,
  ShieldCheck,
  Wifi,
} from "lucide-react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";

const features = [
  {
    icon: Cpu,
    title: "USB autodetect & mode-switch",
    body: "Scans the USB bus for ZeroCD Wi-Fi dongles (UGREEN, AIC, Realtek) and ejects storage mode so the adapter re-enumerates as a WLAN device.",
  },
  {
    icon: Radio,
    title: "Hotspot scanning",
    body: "802.11 beacon and probe scanning across 2.4/5/6 GHz returns SSIDs, BSSIDs, RSSI, channel and security — wired and wireless sources unified.",
  },
  {
    icon: KeyRound,
    title: "WPA2/WPA3 association",
    body: "Select a hotspot and the daemon derives the PMK via PBKDF2 and runs the 4-way EAPOL handshake to the target BSSID.",
  },
  {
    icon: Boxes,
    title: "3-tier hardware topology",
    body: "USB driver → BSD interface (en0, en14, utun4) → network endpoint, with vendor/product IDs, MACs, IPs and link state at every level.",
  },
  {
    icon: Activity,
    title: "Live telemetry",
    body: "Bytes/packets in and out, per-interface download/upload rates in KB/s, and UP/DOWN state polled from netstat.",
  },
  {
    icon: Gauge,
    title: "Diagnostics & speedtest",
    body: "ICMP/TCP ping reachability against gateway and public DNS (1.1.1.1, 8.8.8.8, 9.9.9.9) with RTT and packet loss, plus download/upload throughput.",
  },
  {
    icon: ShieldCheck,
    title: "Uptime & stability",
    body: "Connection duration, disconnect/reconnect counts and a stability score surfaced both in the app and as Prometheus / OpenTelemetry metrics.",
  },
  {
    icon: Wifi,
    title: "MCP server for AI agents",
    body: "A stdio JSON-RPC server exposes scan, connect, telemetry, topology, diagnostics and uptime as six tools your agent can call directly.",
  },
];

export function Features() {
  return (
    <section id="features" className="mx-auto max-w-6xl px-6 py-24">
      <div className="max-w-2xl">
        <p className="font-mono text-sm text-sky-400">// capabilities</p>
        <h2 className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">
          Everything between the USB port and the dish
        </h2>
        <p className="mt-4 text-lg leading-8 text-muted-foreground">
          A Go daemon owns the hardware; a SwiftUI app and an MCP server share
          one HTTP API on localhost.
        </p>
      </div>

      <div className="mt-12 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {features.map((f) => (
          <Card key={f.title} className="card-border-glow bg-card/60">
            <CardHeader>
              <f.icon className="size-5 text-sky-400" />
            </CardHeader>
            <CardContent>
              <h3 className="font-semibold">{f.title}</h3>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">{f.body}</p>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}
