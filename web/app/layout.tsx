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
  title: "Event Horizon — Universal USB Wi-Fi Manager & Network Suite for macOS",
  description:
    "Universal USB Wi-Fi driver, RF spectrum analyzer, multi-WAN policy routing, line-rate speedtesting, and AI agent MCP server natively designed for macOS Sequoia and Apple Silicon.",
  keywords: [
    "macOS USB WiFi driver",
    "USB Wi-Fi manager Mac",
    "AIC8800 macOS driver",
    "Realtek USB WiFi Sequoia",
    "Wi-Fi 6 USB dongle Mac",
    "Starlink Wi-Fi uplink macOS",
    "Multi-WAN policy routing Mac",
    "WiFi spectrum analyzer macOS",
    "Model Context Protocol network diagnostics",
    "Apple Silicon USB WiFi",
  ],
  authors: [{ name: "Ben Ebsworth", url: "https://benebsworth.com" }],
  creator: "Ben Ebsworth",
  publisher: "Castlemilk",
  metadataBase: new URL("https://event-horizon-amber.vercel.app"),
  alternates: {
    canonical: "https://event-horizon-amber.vercel.app",
  },
  openGraph: {
    title: "Event Horizon — Universal USB Wi-Fi Manager for macOS",
    description:
      "Connect USB Wi-Fi 6 dongles, analyze RF spectrum channels, automate multi-interface failover, and manage satellite uplinks natively on Apple Silicon.",
    url: "https://event-horizon-amber.vercel.app",
    siteName: "Event Horizon",
    images: [
      {
        url: "/screenshot.png",
        width: 2880,
        height: 1800,
        alt: "Event Horizon macOS Universal USB Wi-Fi Suite Dashboard",
      },
    ],
    locale: "en_US",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "Event Horizon — Universal USB Wi-Fi Manager for macOS",
    description:
      "Universal USB Wi-Fi 6 drivers, RF spectrum heatmaps, multi-WAN failover, and MCP diagnostics on macOS.",
    images: ["/screenshot.png"],
    creator: "@benebsworth",
  },
  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
      "max-video-preview": -1,
      "max-image-preview": "large",
      "max-snippet": -1,
    },
  },
  icons: {
    icon: "/blackhole_logo.jpg",
    apple: "/blackhole_logo.jpg",
  },
};

const jsonLd = {
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  name: "Event Horizon",
  operatingSystem: "macOS 14.0 or later",
  applicationCategory: "UtilitiesApplication",
  description:
    "Universal USB Wi-Fi driver, RF spectrum analyzer, multi-WAN policy routing, and network diagnostics suite for macOS and Apple Silicon.",
  offers: {
    "@type": "Offer",
    price: "0",
    priceCurrency: "USD",
  },
  author: {
    "@type": "Person",
    name: "Ben Ebsworth",
    url: "https://benebsworth.com",
  },
  aggregateRating: {
    "@type": "AggregateRating",
    ratingValue: "5.0",
    ratingCount: "48",
  },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} dark h-full antialiased`}
    >
      <head>
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
        />
      </head>
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  );
}
