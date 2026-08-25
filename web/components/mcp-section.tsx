import { Badge } from "@/components/ui/badge";

const tools = [
  ["usbwifi_get_hardware_topology", "3-tier USB → interface map"],
  ["usbwifi_get_telemetry", "live rates & interface stats"],
  ["usbwifi_scan_hotspots", "802.11 beacon scan"],
  ["usbwifi_connect_hotspot", "WPA2/WPA3 associate"],
  ["usbwifi_run_diagnostics", "RTT + packet loss"],
  ["usbwifi_get_uptime", "stability & reconnects"],
];

const config = `{
  "mcpServers": {
    "usbwifi": {
      "command": "bin/usbwifi-mcp",
      "args": [],
      "env": { "DAEMON_URL": "http://127.0.0.1:8990" }
    }
  }
}`;

export function McpSection() {
  return (
    <section id="mcp" className="border-y border-border/60 bg-zinc-950/60">
      <div className="mx-auto grid max-w-6xl gap-12 px-6 py-24 lg:grid-cols-2">
        <div>
          <p className="font-mono text-sm text-violet-400">// agent interface</p>
          <h2 className="mt-3 text-3xl font-semibold tracking-tight sm:text-4xl">
            Your AI agent can drive the dongle
          </h2>
          <p className="mt-4 text-lg leading-8 text-muted-foreground">
            Event Horizon ships an MCP server speaking JSON-RPC 2.0 over stdio.
            Point Claude, Gemini or any MCP-capable agent at{" "}
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-sm">
              bin/usbwifi-mcp
            </code>{" "}
            and it can scan, connect and diagnose the link itself — no UI clicks
            required.
          </p>

          <ul className="mt-8 space-y-3">
            {tools.map(([name, desc]) => (
              <li key={name} className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <Badge variant="secondary" className="font-mono text-xs">
                  {name}
                </Badge>
                <span className="text-sm text-muted-foreground">{desc}</span>
              </li>
            ))}
          </ul>
        </div>

        <div className="ring-glow overflow-hidden rounded-xl border bg-black">
          <div className="flex items-center gap-1.5 border-b border-border/60 px-4 py-2.5">
            <span className="size-2.5 rounded-full bg-zinc-700" />
            <span className="size-2.5 rounded-full bg-zinc-700" />
            <span className="size-2.5 rounded-full bg-zinc-700" />
            <span className="ml-3 font-mono text-xs text-muted-foreground">
              .agents/mcp_config.json
            </span>
          </div>
          <pre className="overflow-x-auto p-4 font-mono text-[13px] leading-6 text-zinc-300">
            <code>{config}</code>
          </pre>
        </div>
      </div>
    </section>
  );
}
