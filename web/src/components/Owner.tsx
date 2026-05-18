export function OwnerCell({ email }: { email?: string }) {
  if (!email) return <span className="text-ink-faint">—</span>;
  const initials = email
    .split("@")[0]
    .split(/[._-]+/)
    .slice(0, 2)
    .map((s) => s.charAt(0).toUpperCase())
    .join("") || "·";
  return (
    <span className="inline-flex items-center gap-2">
      <span className="size-5 rounded-full bg-canvas-raised border border-line-subtle flex items-center justify-center text-[10px] text-ink-muted">
        {initials}
      </span>
      <span className="text-ink-DEFAULT truncate max-w-[180px]">{email}</span>
    </span>
  );
}
