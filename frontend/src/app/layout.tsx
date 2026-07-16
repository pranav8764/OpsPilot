import type { Metadata } from "next";
import { ClerkProvider } from '@clerk/nextjs'
import "./globals.css";

export const metadata: Metadata = {
  title: "OpsPilot | Evidence-backed incident-to-fix workflows",
  description: "Turn production logs and repository evidence into a verified, human-approved draft FIX pull request.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <ClerkProvider
      appearance={{
        cssLayerName: 'clerk',
        variables: {
          colorPrimary: '#6366f1',
          colorPrimaryForeground: '#ffffff',
          colorBackground: '#121217',
          colorForeground: '#f4f4f5',
          colorMutedForeground: '#a1a1aa',
          colorInput: '#09090b',
          colorInputForeground: '#fafafa',
          colorBorder: 'rgba(255, 255, 255, 0.15)',
          colorDanger: '#ef4444',
          borderRadius: '0.75rem',
          fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif',
        },
        elements: {
          card: "glass-panel border border-[#ffffff26] bg-[#1a1a2466] shadow-2xl",
          headerTitle: "text-white font-display",
          headerSubtitle: "text-zinc-400",
          socialButtonsBlockButton: "bg-[#ffffff0d] border border-[#ffffff26] text-white hover:bg-[#ffffff1a]",
          socialButtonsBlockButtonText: "text-white font-medium",
          formButtonPrimary: "btn btn-primary w-full",
          formFieldInput: "input-field bg-[#00000033]",
          formFieldLabel: "text-zinc-400",
          footerActionLink: "text-indigo-400 hover:text-indigo-300",
          identityPreviewText: "text-zinc-300",
          userButtonPopoverCard: "bg-[#121217] border border-[#ffffff26]",
          userButtonPopoverActionButton: "hover:bg-[#ffffff1a] text-zinc-300",
          userButtonPopoverActionButtonText: "text-zinc-300",
          userPreviewSecondaryIdentifier: "text-zinc-500",
        }
      }}
    >
      <html lang="en">
        <body>
          {children}
        </body>
      </html>
    </ClerkProvider>
  );
}
