import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Apple, Download, Hammer, PlugZap } from "lucide-react";

const build = `# requires Go 1.22+, Swift 6, Task
git clone https://github.com/castlemilk/event-horizon
cd event-horizon
task build          # builds .app + DMG + MCP server
open "build/Event Horizon.app"`;

const daemon = `# run just the Go daemon + API
go build -o bin/usbwifi ./cmd/usbwifi
./bin/usbwifi --ssid "CNH Starlink" --port 8990
# → http://127.0.0.1:8990/api/wifi/scan`;

const requirements = [
  "macOS 14 or newer, Apple Silicon (arm64)",
  "A USB Wi-Fi dongle: AIC8800, Realtek, or UGREEN",
  "Go 1.22+ and Swift 6 toolchain to build from source",
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
              Signed Apple Silicon build with the Go daemon and libusb bundled
              inside the app bundle.
            </p>
            <a
              href="/downloads/EventHorizon-1.0.0-macOS.dmg"
              download
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
                Run the daemon on its own — the HTTP API is all you need for
                scripting or the MCP server.
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
