import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { CheckCircle2, ArrowRight, Zap, Shield, Activity, BarChart2 } from "lucide-react";

export function SpectrumShowcase() {
  const mock24Channels = [
    { ch: 1, count: 1, status: "Clean", color: "bg-emerald-500", h: "h-14", opt: true },
    { ch: 2, count: 0, status: "Overlap", color: "bg-zinc-600", h: "h-4", opt: false },
    { ch: 3, count: 1, status: "Overlap", color: "bg-amber-500", h: "h-10", opt: false },
    { ch: 4, count: 0, status: "Overlap", color: "bg-zinc-600", h: "h-4", opt: false },
    { ch: 5, count: 2, status: "Overlap", color: "bg-orange-500", h: "h-16", opt: false },
    { ch: 6, count: 4, status: "Congested", color: "bg-red-500", h: "h-24", opt: false },
    { ch: 7, count: 1, status: "Overlap", color: "bg-amber-500", h: "h-8", opt: false },
    { ch: 8, count: 0, status: "Overlap", color: "bg-zinc-600", h: "h-4", opt: false },
    { ch: 9, count: 1, status: "Overlap", color: "bg-amber-500", h: "h-10", opt: false },
    { ch: 10, count: 0, status: "Overlap", color: "bg-zinc-600", h: "h-4", opt: false },
    { ch: 11, count: 1, status: "Clean", color: "bg-emerald-500", h: "h-12", opt: true },
  ];

  return (
    <section className="mx-auto max-w-6xl px-6 py-20 border-t border-border/40">
      <div className="grid gap-12 lg:grid-cols-2 items-center">
        {/* Left: Explanatory Copy */}
        <div>
          <Badge variant="outline" className="mb-4 gap-1.5 rounded-full px-3 py-1 font-mono text-xs text-cyan-400 border-cyan-500/30 bg-cyan-500/10">
            <BarChart2 className="size-3.5" />
            Phase 1 & 2 Capabilities
          </Badge>
          <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl text-gradient">
            Spectrum Telemetry & Multi-WAN Failover
          </h2>
          <p className="mt-4 text-base leading-7 text-muted-foreground">
            Eliminate Wi-Fi congestion and connection drops. Event Horizon continuously profiles 2.4 GHz and 5 GHz spectrum channels while managing dual-interface default gateway priority.
          </p>

          <div className="mt-6 space-y-4">
            <div className="flex items-start gap-3">
              <div className="rounded-lg bg-emerald-500/10 p-1.5 text-emerald-400 mt-0.5">
                <CheckCircle2 className="size-4" />
              </div>
              <div>
                <h4 className="text-sm font-semibold text-foreground">1-Click Clean Channel Recommendation</h4>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Calculates co-channel interference and adjacent channel overlap to pinpoint the optimal channel for your router.
                </p>
              </div>
            </div>

            <div className="flex items-start gap-3">
              <div className="rounded-lg bg-purple-500/10 p-1.5 text-purple-400 mt-0.5">
                <Zap className="size-4" />
              </div>
              <div>
                <h4 className="text-sm font-semibold text-foreground">Zero-Stall Gateway Failover Watchdog</h4>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Automatically switches system routing from host Wi-Fi to Starlink USB Wi-Fi if gateway ICMP pings fail.
                </p>
              </div>
            </div>

            <div className="flex items-start gap-3">
              <div className="rounded-lg bg-sky-500/10 p-1.5 text-sky-400 mt-0.5">
                <Activity className="size-4" />
              </div>
              <div>
                <h4 className="text-sm font-semibold text-foreground">Multi-Stream Line-Rate Speedtest</h4>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Benchmark interface-bound throughput up to 1+ Gbps with sub-millisecond bufferbloat jitter measurement.
                </p>
              </div>
            </div>
          </div>
        </div>

        {/* Right: Mock Interactive Spectrum Card */}
        <div className="relative">
          <div className="overflow-hidden rounded-xl border border-border/80 bg-zinc-950/90 p-6 shadow-2xl backdrop-blur">
            <div className="flex items-center justify-between border-b border-border/60 pb-4">
              <div className="flex items-center gap-2">
                <div className="size-2 rounded-full bg-emerald-400 animate-pulse" />
                <span className="font-mono text-xs font-semibold text-zinc-300">2.4 GHz RF Spectrum Visualizer</span>
              </div>
              <span className="rounded-md bg-emerald-500/15 px-2 py-0.5 font-mono text-[11px] font-semibold text-emerald-400">
                Optimal: Ch 1 & 11
              </span>
            </div>

            {/* Spectrum Visual Bars */}
            <div className="mt-8 flex items-end justify-between gap-1.5 h-32 px-2 pb-2 bg-zinc-900/50 rounded-lg border border-zinc-800/80">
              {mock24Channels.map((item) => (
                <div key={item.ch} className="flex flex-col items-center gap-1.5 flex-1">
                  <span className="font-mono text-[9px] text-zinc-400">
                    {item.count > 0 ? `${item.count}` : "—"}
                  </span>
                  <div className="w-full flex items-end justify-center h-24">
                    <div
                      className={`w-full max-w-[18px] rounded-t-sm transition-all duration-500 ${item.color} ${item.h} ${item.opt ? "ring-2 ring-emerald-400/50 shadow-[0_0_12px_rgba(16,185,129,0.5)]" : ""}`}
                    />
                  </div>
                  <span className={`font-mono text-[10px] ${item.opt ? "font-bold text-emerald-400" : "text-zinc-500"}`}>
                    {item.ch}
                  </span>
                </div>
              ))}
            </div>

            {/* Live Routing Policy Status Pill */}
            <div className="mt-5 grid grid-cols-2 gap-3">
              <div className="rounded-lg bg-zinc-900/80 p-3 border border-zinc-800">
                <div className="text-[10px] font-mono text-zinc-400">ACTIVE DEFAULT ROUTE</div>
                <div className="mt-1 flex items-center gap-2 font-mono text-xs font-bold text-emerald-400">
                  <span className="size-1.5 rounded-full bg-emerald-400" />
                  en0 (192.168.1.105)
                </div>
              </div>
              <div className="rounded-lg bg-zinc-900/80 p-3 border border-zinc-800">
                <div className="text-[10px] font-mono text-zinc-400">FAILOVER BACKUP LINK</div>
                <div className="mt-1 flex items-center gap-2 font-mono text-xs font-bold text-purple-400">
                  <span className="size-1.5 rounded-full bg-purple-400" />
                  utun10 (192.168.100.2)
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
