import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Bet9ja VFL Private Analytics",
  description: "Private, auditable VFL prediction and analytics dashboard.",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className="dark">
      <body>{children}</body>
    </html>
  );
}
