import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ArrowRight, BookOpen, ShieldOff } from "lucide-react";

export const WRITEUP_URL = "https://benebsworth.com/blog/usb-wifi-in-userspace/";

const identities = [
  { id: "a69c:5723", name: "ZeroCD", kind: "USB mass storage", step: "SCSI eject" },
  { id: "a69c:8d80", name: "Boot ROM", kind: "waiting for firmware", step: "upload 4 blobs" },
  { id: "368b:8d85", name: "Operational", kind: "the real product identity", step: null },
];

const layers = [
  { layer: "IP", does: "DHCP client, ARP, utun bridge", normally: "the network stack", kernel: true },
  { layer: "802.11 → Ethernet", does: "MPDU header, LLC/SNAP, CCMP strip", normally: "802.11 stack", kernel: true },
  { layer: "WPA2 4-way handshake", does: "EAPOL-Key, PTK/GTK, RFC 3394 unwrap", normally: "wpa_supplicant", kernel: false },
  { layer: "Scan + associate", does: "probe, auth, assoc, SM_CONNECT_REQ", normally: "CoreWLAN", kernel: true },
  { layer: "MAC bring-up", does: "reset, me_config, chan_config, start, add_if", normally: "802.11 stack", kernel: true },
  { layer: "RF / PHY calibration", does: "per-chip RF tables, tx power", normally: "vendor kext", kernel: true },
  { layer: "LMAC control plane", does: "MM/ME/SM messages, TLV config, ACKs", normally: "vendor kext", kernel: true },
  { layer: "Firmware bootstrap", does: "ZeroCD eject, boot ROM upload", normally: "vendor kext", kernel: true },
  { layer: "USB transport", does: "bulk endpoints, record framing, resync", normally: "kernel USB stack", kernel: true },
];

const walls = [
  ["EAPOL-Key descriptor 8 bytes short", "round-tripped our own encoder"],
  ["8-byte aggregated USB TX header", "matched code the reference never compiles"],
  ["TX pipe alternation dropping msg4", "a debug probe nobody retired"],
  ["RX decoder expecting Ethernet at offset 60", "asserted the wrong layout, passed throughout"],
];

const stages = [
  { name: "flash", ms: 344 },
  { name: "firmware upload", ms: 26500 },
  { name: "associate", ms: 2290 },
  { name: "WPA2 handshake", ms: 107 },
  { name: "DHCP + bridge", ms: 3050 },
];
const weights = stages.map((s) => Math.sqrt(s.ms));
const wsum = weights.reduce((a, b) => a + b, 0);
const fmt = (ms: number) => (ms >= 1000 ? `${(ms / 1000).toFixed(ms >= 10000 ? 1 : 2)} s` : `${ms} ms`);

export function HowItWorks() {
  return (
    <section id="how-it-works" className="border-y border-border/60 bg-zinc-950/60">
      <div className="mx-auto max-w-6xl px-6 py-24">
        <div className="max-w-3xl">
          <p className="font-mono text-sm text-amber-400">// how it works</p>
          <h2 className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">
            A Wi-Fi driver that never touches the kernel
          </h2>
          <p className="mt-4 text-lg leading-8 text-muted-foreground">
            It started with an ordinary UGREEN AX900 dongle that works on any
            Windows laptop and does nothing on a Mac. Apple ships no driver,
            the vendor ships no driver. Event Horizon drives
            the AIC8800D80 radio entirely from user space: libusb at the bottom,
            a <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-sm">utun</code> at
            the top, and every layer in between reimplemented in Go. System
            Integrity Protection stays on. No kernel extension, no DriverKit.
          </p>
          <div className="mt-6 flex flex-wrap items-center gap-3">
            <Button asChild size="lg">
              <a href={WRITEUP_URL} target="_blank" rel="noreferrer">
                <BookOpen className="size-4" />
                Read the full write-up
                <ArrowRight className="size-4" />
              </a>
            </Button>
            <span className="text-sm text-muted-foreground">
              Firmware carving, the LMAC control plane, WPA2 from scratch, and a month of wrong turns.
            </span>
          </div>
        </div>

        <div className="mt-14 grid gap-5 lg:grid-cols-[1.1fr_0.9fr]">
          {/* Layer stack */}
          <Card className="card-border-glow bg-card/60">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <h3 className="font-semibold">Nine layers, one Go process</h3>
                <Badge variant="secondary" className="font-mono text-[10px]">
                  <ShieldOff className="mr-1 size-3" /> 0 kexts
                </Badge>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                Amber rows are what the kernel, or a vendor kernel extension, would normally own.
              </p>
              <ol className="mt-4 flex flex-col gap-1">
                {layers.map((l, i) => (
                  <li
                    key={l.layer}
                    className={
                      "grid grid-cols-[1.1fr_1.5fr_0.9fr] items-center gap-3 rounded-md border-l-2 px-3 py-1.5 text-[13px] " +
                      (l.kernel ? "border-amber-400 bg-amber-400/8" : "border-border bg-transparent")
                    }
                  >
                    <span className="font-medium text-foreground">
                      <span className="mr-2 font-mono text-[10px] text-muted-foreground">
                        {String(layers.length - i).padStart(2, "0")}
                      </span>
                      {l.layer}
                    </span>
                    <span className="font-mono text-[11px] leading-snug text-muted-foreground">{l.does}</span>
                    <span className="text-right text-[11px] text-muted-foreground">{l.normally}</span>
                  </li>
                ))}
              </ol>
            </CardContent>
          </Card>

          <div className="flex flex-col gap-5">
            {/* Identity chain */}
            <Card className="card-border-glow bg-card/60">
              <CardContent className="pt-6">
                <h3 className="font-semibold">One dongle, three USB identities</h3>
                <p className="mt-1 text-sm text-muted-foreground">
                  It enumerates as a disk. The Wi-Fi adapter only appears after a SCSI
                  eject and a 26 s firmware upload, under a different vendor ID.
                </p>
                <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:items-center">
                  {identities.map((s) => (
                    <div key={s.id} className="flex flex-1 items-center gap-2">
                      <div className="flex-1 rounded-lg border border-border/60 bg-black/40 px-3 py-2">
                        <div className="font-mono text-sm font-semibold">{s.id}</div>
                        <div className="text-xs text-foreground/85">{s.name}</div>
                        <div className="text-[11px] text-muted-foreground">{s.kind}</div>
                      </div>
                      {s.step && (
                        <div className="flex shrink-0 flex-col items-center text-amber-400">
                          <ArrowRight className="size-4" />
                          <span className="mt-0.5 w-16 text-center font-mono text-[9px] leading-tight text-muted-foreground">
                            {s.step}
                          </span>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>

            {/* Bring-up timeline */}
            <Card className="card-border-glow bg-card/60">
              <CardContent className="pt-6">
                <div className="flex items-baseline justify-between">
                  <h3 className="font-semibold">Cold replug to routed traffic</h3>
                  <span className="font-mono text-sm text-amber-400">32.3 s</span>
                </div>
                <div className="mt-3 flex h-7 w-full overflow-hidden rounded-md border border-border/60">
                  {stages.map((s, i) => (
                    <div
                      key={s.name}
                      title={`${s.name}: ${fmt(s.ms)}`}
                      className="flex items-center justify-center border-r border-black/60 bg-amber-400 last:border-r-0"
                      style={{ width: `${(weights[i] / wsum) * 100}%`, opacity: 0.4 + 0.5 * (i / (stages.length - 1)) }}
                    >
                      {weights[i] / wsum > 0.09 && (
                        <span className="truncate px-1 font-mono text-[10px] font-semibold text-black/80">{fmt(s.ms)}</span>
                      )}
                    </div>
                  ))}
                </div>
                <p className="mt-2 text-[11px] text-muted-foreground">
                  flash · firmware upload · associate · WPA2 handshake · DHCP + bridge. Square-root
                  scale; the upload is over 80% of the wall clock.
                </p>
              </CardContent>
            </Card>
          </div>
        </div>

        {/* Four walls */}
        <Card className="card-border-glow mt-5 bg-card/60">
          <CardContent className="pt-6">
            <div className="grid gap-6 lg:grid-cols-[0.8fr_1.2fr]">
              <div>
                <h3 className="font-semibold">Four walls, every one with a passing test</h3>
                <p className="mt-2 text-sm leading-6 text-muted-foreground">
                  Every blocker on the data path was a wrong assumption that our own tests
                  agreed with. A self-consistent wrong wire format round-trips perfectly. The
                  rule that came out of it, now in the repo&apos;s debugging skill: never validate
                  a wire format against your own encoder.
                </p>
              </div>
              <ul className="divide-y divide-border/60 rounded-lg border border-border/60">
                {walls.map(([wall, test]) => (
                  <li key={wall} className="grid gap-1 px-4 py-2.5 text-[13px] sm:grid-cols-2 sm:gap-4">
                    <span className="font-medium text-foreground">{wall}</span>
                    <span className="font-mono text-[11px] text-muted-foreground">{test}</span>
                  </li>
                ))}
              </ul>
            </div>
          </CardContent>
        </Card>
      </div>
    </section>
  );
}
