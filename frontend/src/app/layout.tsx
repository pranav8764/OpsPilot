import type { Metadata } from "next";
import "./globals.css";
import { ThemeProvider } from "@/components/theme-provider";
import { ClerkThemeProvider } from "@/components/clerk-theme-provider";

export const metadata: Metadata = {
  title: "Archiatros | Evidence-backed incident-to-fix workflows",
  description: "Turn production logs and repository evidence into a verified, human-approved draft FIX pull request.",
};

// Runs before hydration so the correct theme paints on first frame — no
// flash of the wrong theme. Kept tiny and defensive (localStorage can throw
// in private-browsing contexts).
const THEME_INIT_SCRIPT = `(function(){try{var t=localStorage.getItem('archiatros-theme');if(t==='dark'){document.documentElement.setAttribute('data-theme','dark');}}catch(e){}})();`;

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
      </head>
      <body>
        <ThemeProvider>
          <ClerkThemeProvider>
            {children}
          </ClerkThemeProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
