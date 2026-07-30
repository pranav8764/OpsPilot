import { SignUp } from "@clerk/nextjs";
import { ThemeToggle } from "@/components/theme-toggle";

export default function Page() {
  return (
    <div className="flex items-center justify-center min-h-screen bg-[var(--bg-primary)] relative">
      {/* Background glow */}
      <div className="absolute top-[20%] left-[-10%] w-[40%] h-[40%] bg-[var(--accent-primary)]/10 rounded-full blur-[100px] pointer-events-none" />
      <div className="absolute top-6 right-6 z-10">
        <ThemeToggle />
      </div>
      <SignUp path="/sign-up" routing="path" signInUrl="/sign-in" />
    </div>
  );
}
