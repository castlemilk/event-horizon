import { GithubIcon } from "@/components/github-icon";
import { Blackhole } from "@/components/blackhole";

export function SiteFooter() {
  return (
    <footer className="border-t border-border/60">
      <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-4 px-6 py-10 sm:flex-row">
        <div className="flex items-center gap-2.5">
          <Blackhole className="h-5 w-5" />
          <span className="font-mono text-sm">event-horizon</span>
        </div>
        <p className="text-sm text-muted-foreground">
          © {new Date().getFullYear()} CastleMilk · Open source · MIT
        </p>
        <a
          href="https://github.com/castlemilk/event-horizon"
          target="_blank"
          rel="noreferrer"
          className="text-muted-foreground transition-colors hover:text-foreground"
          aria-label="GitHub repository"
        >
          <GithubIcon className="size-5" />
        </a>
      </div>
    </footer>
  );
}
