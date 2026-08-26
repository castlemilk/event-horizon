import { SiteNav } from "@/components/site-nav";
import { Hero } from "@/components/hero";
import { Features } from "@/components/features";
import { SpectrumShowcase } from "@/components/spectrum-showcase";
import { McpSection } from "@/components/mcp-section";
import { InstallSection } from "@/components/install-section";
import { SiteFooter } from "@/components/site-footer";

export default function Home() {
  return (
    <div className="flex flex-1 flex-col bg-space">
      <SiteNav />
      <main className="flex-1">
        <Hero />
        <Features />
        <SpectrumShowcase />
        <McpSection />
        <InstallSection />
      </main>
      <SiteFooter />
    </div>
  );
}
