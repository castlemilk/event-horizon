import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Apple, Download, Hammer, PlugZap } from "lucide-react";

const build = `# requires Go 1.22+, Swift 6, Task
git clone https://github.com/castlemilk/event-horizon
cd event-horizon
task build          # builds .app + DMG + MCP server
open "build/Event Horizon.app"`;

const daemon = `# one-time firmware step (brew install innoextract)
EH="/Applications/Event Horizon.app/Contents/Resources"
"$EH/usbwifi" firmware fetch    # 3 public blobs, SHA-256 verified
"$EH/usbwifi" firmware carve    # 4th blob, from the dongle's ZeroCD volume

# bring the link up from the CLI
sudo "$EH/usbwifi" cmdctl link --ssid "<network>" --pass '<pw>'`;

const requirements = [
  "macOS 14 or newer, Apple Silicon (arm64)",
  "A UGREEN AX900 (AICSEMI AIC8800D80) USB Wi-Fi dongle",
  "innoextract (brew install innoextract) for the one-time firmware step",
  "Go 1.22+ and Swift 6 only if you build from source",
];

export function InstallSection() {
  return (
    <section id="install" className="mx-auto max-w-6xl px-6 py-24">
      <div className="max-w-2xl">
        <p className="font-mono text-sm text-emerald-400">// install</p>
        <h2 className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">
          Up and running in under a minute
        </h2>
        <p className="mt-4 text-lg leading-8 text-muted-foreground">
          Grab the disk image for the quickest path, or build from source to
          stay on the bleeding edge.
        </p>
      </div>

      <div className="mt-12 grid gap-4 lg:grid-cols-2">
        <Card className="card-border-glow bg-card/60">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <Download className="size-4 text-sky-400" />
              Download the disk image
            </CardTitle>
            <Badge variant="secondary" className="font-mono">v1.0.0</Badge>
          </CardHeader>
          <CardContent>
            <p className="text-sm leading-6 text-muted-foreground">
              Apple Silicon build, signed with Developer ID and notarised by
              Apple. The Go daemon, the MCP server and libusb are inside the
              app bundle. Firmware is not: a one-time{" "}
              <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">firmware fetch</code>
              {" "}+{" "}
              <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">firmware carve</code>
              {" "}produces it from the dongle&apos;s own driver.
            </p>
            <a
              href="https://github.com/castlemilk/event-horizon/releases/download/v1.0.0/EventHorizon-1.0.0-macOS.dmg"
              className="mt-4 inline-flex items-center gap-2 rounded-lg bg-foreground px-4 py-2 text-sm font-medium text-background transition-opacity hover:opacity-90"
            >
              <Apple className="size-4" />
              EventHorizon-1.0.0-macOS.dmg
            </a>
          </CardContent>
        </Card>

        <Card className="card-border-glow bg-card/60">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <Hammer className="size-4 text-sky-400" />
              Build from source
            </CardTitle>
            <Badge variant="secondary" className="font-mono">git main</Badge>
          </CardHeader>
          <CardContent>
            <pre className="overflow-x-auto rounded-lg border border-border/60 bg-black p-4 font-mono text-[13px] leading-6 text-zinc-300">
              <code>{build}</code>
            </pre>
          </CardContent>
        </Card>

        <Card className="card-border-glow bg-card/60 lg:col-span-2">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <PlugZap className="size-4 text-sky-400" />
              Daemon + MCP quickstart
            </CardTitle>
            <Badge variant="secondary" className="font-mono">headless</Badge>
          </CardHeader>
          <CardContent className="grid gap-6 lg:grid-cols-2">
            <div>
              <p className="mb-3 text-sm text-muted-foreground">
                The app does the same thing with a button, but everything is
                scriptable from the bundled CLI, and the daemon&apos;s HTTP API on
                :8990 is what the MCP server talks to.
              </p>
              <pre className="overflow-x-auto rounded-lg border border-border/60 bg-black p-4 font-mono text-[13px] leading-6 text-zinc-300">
                <code>{daemon}</code>
              </pre>
            </div>
            <ul className="space-y-2 text-sm text-muted-foreground">
              {requirements.map((r) => (
                <li key={r} className="flex gap-2">
                  <span className="text-emerald-400">✓</span>
                  {r}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      </div>
    </section>
  );
}
