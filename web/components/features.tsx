import {
  Activity,
  Boxes,
  Cpu,
  Gauge,
  KeyRound,
  Radio,
  ShieldCheck,
  Wifi,
  ArrowLeftRight,
  BarChart3,
  Globe,
  Bot,
} from "lucide-react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";

const features = [
  {
    icon: Cpu,
    title: "USB Autodetect & ZeroCD Eject",
    badge: "Hardware HAL",
    body: "Scans the USB bus for Wi-Fi dongles (AicSemi AIC8800 Wi-Fi 6, Realtek RTL8811/12/21/32, MediaTek MT7601U) and auto-ejects storage mode into native WLAN mode.",
  },
  {
    icon: BarChart3,
    title: "RF Spectrum & Channel Analyzer",
    badge: "Wi-Fi 6 / 2.4 & 5 GHz",
    body: "Live 2.4 GHz & 5 GHz spectrum channel occupancy graphs, co-channel density penalty scoring, and automated 1-click clean channel recommendations.",
  },
  {
    icon: ArrowLeftRight,
    title: "Multi-WAN Policy & Auto-Failover",
    badge: "Smart Routing",
    body: "Manage default gateway priority between Host Wi-Fi (en0) and USB Wi-Fi (utun10) with an automated watchdog that fails over seamlessly if pings drop.",
  },
  {
    icon: Gauge,
    title: "Multi-Stream Speedtest Engine",
    badge: "Line-Rate Bandwidth",
    body: "Concurrent multi-worker HTTP download & upload throughput benchmarking, interface-bound socket testing, and sub-millisecond bufferbloat jitter measurement.",
  },
  {
    icon: Globe,
    title: "L4 User-Space Protocol Bridge",
    badge: "utun10 Architecture",
    body: "Full TCP 3-way handshake responder, UDP datagram multiplexing, RFC 1035 DNS A-record resolver, and RFC 5905 NTP time synchronization on virtual tunnel interfaces.",
  },
  {
    icon: KeyRound,
    title: "WPA2/WPA3 Hardware Handshakes",
    badge: "Enterprise Security",
    body: "Derives cryptographic PMK keys via PBKDF2 and executes the 4-way EAPOL handshake directly against target access points and Starlink hotspots.",
  },
  {
    icon: Boxes,
    title: "3-Tier Hardware Topology Map",
    badge: "macOS Core",
    body: "Real-time visual map bridging USB Physical Driver → macOS BSD Interface (en0, en14, utun10) → Network Endpoint with live link state at every tier.",
  },
  {
    icon: Bot,
    title: "Model Context Protocol (MCP)",
    badge: "AI Native",
    body: "Integrated JSON-RPC 2.0 stdio server equipping Claude Desktop, Cursor, Antigravity, and Codex with autonomous network diagnostics and Wi-Fi management tools.",
  },
];

export function Features() {
  return (
    <section id="features" className="mx-auto max-w-6xl px-6 py-24">
      <div className="max-w-3xl">
        <p className="font-mono text-sm text-sky-400">// capabilities</p>
        <h2 className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">
          Complete Universal Wi-Fi & Satellite Network Suite
        </h2>
        <p className="mt-4 text-lg leading-8 text-muted-foreground">
          From raw USB bulk endpoints to user-space virtual network tunnels, RF channel heatmaps, and autonomous AI fleet management.
        </p>
      </div>

      <div className="mt-12 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
        {features.map((f) => (
          <Card key={f.title} className="card-border-glow bg-card/60 transition-all duration-300 hover:border-sky-500/40 hover:bg-card/80">
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <div className="rounded-lg bg-sky-500/10 p-2 text-sky-400">
                <f.icon className="size-5" />
              </div>
              <span className="rounded-full bg-secondary/80 px-2 py-0.5 font-mono text-[10px] font-medium text-muted-foreground">
                {f.badge}
              </span>
            </CardHeader>
            <CardContent>
              <h3 className="font-semibold text-foreground">{f.title}</h3>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">{f.body}</p>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}
