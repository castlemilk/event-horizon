import Link from "next/link";
import { Download } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Blackhole } from "@/components/blackhole";
import { GithubIcon } from "@/components/github-icon";

const links = [
  { href: "#features", label: "Features" },
  { href: "#telemetry", label: "Telemetry" },
  { href: "#mcp", label: "MCP" },
  { href: "#install", label: "Install" },
];

export function SiteNav() {
  return (
    <header className="sticky top-0 z-50 border-b border-border/60 bg-background/75 backdrop-blur-md">
      <div className="mx-auto flex h-14 max-w-6xl items-center justify-between gap-6 px-6">
        <Link href="#" className="flex items-center gap-2.5">
          <Blackhole className="h-6 w-6" />
          <span className="font-mono text-sm font-semibold tracking-tight">event-horizon</span>
        </Link>
        <nav className="hidden items-center gap-6 text-sm text-muted-foreground md:flex">
          {links.map((l) => (
            <Link key={l.href} href={l.href} className="transition-colors hover:text-foreground">
              {l.label}
            </Link>
          ))}
        </nav>
        <div className="flex items-center gap-2">
          <Button asChild variant="ghost" size="sm" className="hidden sm:inline-flex">
            <a href="https://github.com/castlemilk/event-horizon" target="_blank" rel="noreferrer">
              <GithubIcon className="size-4" />
              GitHub
            </a>
          </Button>
          <Button asChild size="sm">
            <a href="/downloads/EventHorizon-1.0.0-macOS.dmg" download>
              <Download className="size-4" />
              Download .dmg
            </a>
          </Button>
        </div>
      </div>
    </header>
  );
}
