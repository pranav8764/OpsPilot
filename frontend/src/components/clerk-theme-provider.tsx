'use client';

import { ClerkProvider } from '@clerk/nextjs';
import { useTheme } from '@/components/theme-provider';

const LIGHT_VARS = {
  colorPrimary: '#C98A9E',
  colorPrimaryForeground: '#FFFDFB',
  colorBackground: '#FFFDFB',
  colorInput: '#FBF7F2',
  colorForeground: '#3A332E',
  colorMutedForeground: '#6B615A',
  colorDanger: '#C0605C',
  colorSuccess: '#6E9B69',
  colorWarning: '#BE8A3E',
  colorNeutral: '#948A82',
};

const DARK_VARS = {
  colorPrimary: '#D99CAE',
  colorPrimaryForeground: '#211B16',
  colorBackground: '#241E19',
  colorInput: '#1F1A15',
  colorForeground: '#F3E9DD',
  colorMutedForeground: '#C9BBAA',
  colorDanger: '#E08D87',
  colorSuccess: '#8AB884',
  colorWarning: '#D4A059',
  colorNeutral: '#93887A',
};

export function ClerkThemeProvider({ children }: { children: React.ReactNode }) {
  const { theme } = useTheme();
  const themeVars = theme === 'dark' ? DARK_VARS : LIGHT_VARS;

  return (
    <ClerkProvider
      appearance={{
        cssLayerName: 'clerk',
        variables: {
          ...themeVars,
          borderRadius: '0.875rem',
          fontFamily: 'Work Sans, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif',
        },
        elements: {
          card: "glass-panel border border-[var(--border-light)] bg-[var(--bg-card)] shadow-xl",
          headerTitle: "text-[var(--text-primary)] font-serif",
          headerSubtitle: "text-[var(--text-secondary)]",
          socialButtonsBlockButton: "bg-[var(--bg-secondary)] border border-[var(--border-light)] text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)]",
          socialButtonsBlockButtonText: "text-[var(--text-primary)] font-medium",
          formButtonPrimary: "btn btn-primary w-full",
          formFieldInput: "input-field",
          formFieldLabel: "text-[var(--text-secondary)]",
          footerActionLink: "text-[var(--accent-primary-hover)] hover:text-[var(--accent-primary)]",
          identityPreviewText: "text-[var(--text-secondary)]",
          userButtonPopoverCard: "bg-[var(--bg-card)] border border-[var(--border-light)]",
          userButtonPopoverActionButton: "hover:bg-[var(--bg-tertiary)] text-[var(--text-secondary)]",
          userButtonPopoverActionButtonText: "text-[var(--text-secondary)]",
          userPreviewSecondaryIdentifier: "text-[var(--text-muted)]",
        },
      }}
    >
      {children}
    </ClerkProvider>
  );
}
