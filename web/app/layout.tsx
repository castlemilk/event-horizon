import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Event Horizon — USB Wi-Fi Dongle Manager for macOS",
  description:
    "Detect, mode-switch, and connect USB Wi-Fi dongles on macOS. WPA2/WPA3 handshakes, live telemetry, hardware topology, diagnostics, and an MCP server for AI agents.",
  metadataBase: new URL("https://event-horizon.vercel.app"),
  openGraph: {
    title: "Event Horizon — USB Wi-Fi Dongle Manager for macOS",
    description:
      "Turn a USB Wi-Fi dongle into a Starlink uplink. Autodetect hardware, negotiate WPA2/WPA3, stream live telemetry, and drive everything from an AI agent via MCP.",
    type: "website",
    images: [{ url: "/screenshot.png", width: 1200, height: 630 }],
  },
  icons: { icon: "/blackhole_logo.jpg" },
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} dark h-full antialiased`}
    >
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  );
}
