import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Blackhole } from "@/components/blackhole";
import { Download, Terminal } from "lucide-react";

export function Hero() {
  return (
    <section className="relative overflow-hidden">
      <div className="pointer-events-none absolute inset-0 bg-grid-faint [mask-image:radial-gradient(ellipse_70%_60%_at_50%_-10%,black,transparent)]" />
      <div className="relative mx-auto max-w-6xl px-6 pt-16 pb-0">
        <div className="grid items-center gap-12 lg:grid-cols-[1.05fr_0.95fr]">
          <div>
            <Badge variant="outline" className="mb-6 gap-2 rounded-full px-3 py-1 font-mono text-xs">
              <span className="size-1.5 rounded-full bg-emerald-400 shadow-[0_0_8px_2px_oklch(0.72_0.17_150_/0.7)]" />
              macOS 14+ · Apple Silicon · USB Wi-Fi Dongle Manager
            </Badge>

            <h1 className="text-5xl font-semibold tracking-tight sm:text-6xl">
              <span className="text-gradient">Event Horizon</span>
            </h1>

            <p className="mt-4 max-w-xl text-lg leading-8 text-muted-foreground">
              A standard USB Wi-Fi dongle does not work on a Mac. So we made
              it work: a driver that never touches the kernel. Flash the
              firmware, bring up the radio, run the WPA2 handshake and bridge
              the link into a utun, all from user space with SIP on. Then
              drive it from an AI agent over MCP.
            </p>

            <div className="mt-8 flex flex-wrap items-center gap-3">
              <Button asChild size="lg">
                <a href="/downloads/EventHorizon-1.0.0-macOS.dmg" download>
                  <Download className="size-4" />
                  Download for macOS
                </a>
              </Button>
              <Button asChild size="lg" variant="secondary">
                <a href="#install">
                  <Terminal className="size-4" />
                  Quick start
                </a>
              </Button>
            </div>

            <dl className="mt-10 grid max-w-xl grid-cols-3 gap-6 border-t border-border/60 pt-6">
              {[
                ["0 kexts", "SIP on, user space only"],
                ["WPA2", "4-way EAPOL, from scratch"],
                ["6", "MCP tools"],
              ].map(([k, v]) => (
                <div key={k}>
                  <dt className="font-mono text-lg font-semibold">{k}</dt>
                  <dd className="text-sm text-muted-foreground">{v}</dd>
                </div>
              ))}
            </dl>
          </div>

          <div className="relative">
            <div className="pointer-events-none absolute -inset-10 opacity-60">
              <Blackhole className="h-full w-full" />
            </div>
            <div className="relative ring-glow overflow-hidden rounded-xl border bg-background/80 backdrop-blur">
              <div className="flex items-center gap-1.5 border-b border-border/60 px-4 py-2.5">
                <span className="size-2.5 rounded-full bg-zinc-700" />
                <span className="size-2.5 rounded-full bg-zinc-700" />
                <span className="size-2.5 rounded-full bg-zinc-700" />
                <span className="ml-3 font-mono text-xs text-muted-foreground">
                  usbwifi — daemon :8990
                </span>
              </div>
              <pre className="overflow-x-auto p-4 font-mono text-[13px] leading-6 text-zinc-300">
                <code>
                  <span className="text-zinc-600">$ sudo bin/usbwifi cmdctl link --ssid "Uncle Rad-Guest" --route 192.168.100.1</span>
                  {"\n"}
                  <span className="text-sky-400">[USB]</span>{" "}
                  a69c:5723 ZeroCD → a69c:8d80 boot ROM → 368b:8d85 operational
                  {"\n"}
                  <span className="text-sky-400">[WIFI]</span>{" "}
                  CONNECTED bssid=d2:e8:f0:50:f8:32 freq=2412
                  {"\n"}
                  <span className="text-emerald-400">[WPA2]</span>{" "}
                  msg3 MIC ok · GTK unwrapped · PTK/GTK installed · control port open
                  {"\n"}
                  <span className="text-emerald-400">[NET]</span>{" "}
                  offer 192.168.2.243 · gateway 74:24:9f:44:28:1f · 4/4 pings
                  {"\n"}
                  <span className="text-emerald-400">[LINK]</span>{" "}
                  {"\u2713"} up · bridged on utun11 · route 192.168.100.1 → dish
                  {"\n"}
                  <span className="text-zinc-600">$ bin/usbwifi-mcp</span>
                  {"\n"}
                  <span className="text-violet-400">MCP</span>{" "}
                  6 tools registered · stdio · JSON-RPC 2.0
                </code>
              </pre>
            </div>
          </div>
        </div>

        <div className="relative mt-16 pb-0">
          <div className="ring-glow overflow-hidden rounded-xl border bg-black">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/screenshot.png"
              alt="Event Horizon app dashboard showing hardware topology, live telemetry and the USB Wi-Fi connection panel"
              className="w-full"
            />
          </div>
        </div>
      </div>
    </section>
  );
}
