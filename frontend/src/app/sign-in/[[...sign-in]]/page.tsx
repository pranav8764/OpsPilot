import { SignIn } from "@clerk/nextjs";

export default function Page() {
  return (
    <div className="flex items-center justify-center min-h-screen bg-[var(--bg-primary)]">
      {/* Background glow */}
      <div className="absolute top-[20%] right-[-10%] w-[40%] h-[40%] bg-indigo-600/10 rounded-full blur-[100px] pointer-events-none" />
      <SignIn path="/sign-in" routing="path" signUpUrl="/sign-up" />
    </div>
  );
}
