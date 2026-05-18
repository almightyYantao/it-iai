import type { ReactNode } from "react";
import { cn } from "../lib/cn";

type Tone = "default" | "ok" | "flight" | "warn" | "fail" | "idle";

const valueClass: Record<Tone, string> = {
  default: "text-ink-strong",
  ok:      "text-status-ok-fg",
  flight:  "text-status-flight-fg",
  warn:    "text-status-warn-fg",
  fail:    "text-status-fail-fg",
  idle:    "text-ink-muted",
};

export function Tile({
  label,
  value,
  hint,
  tone = "default",
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  tone?: Tone;
}) {
  return (
    <div className="rounded-lg border border-line-subtle bg-canvas-surface px-5 py-4">
      <div className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">{label}</div>
      <div className="mt-2 flex items-baseline gap-2">
        <div className={cn("text-[28px] font-semibold leading-none tabular-nums", valueClass[tone])}>{value}</div>
        {hint && <div className="text-[12.5px] text-ink-muted">{hint}</div>}
      </div>
    </div>
  );
}

export function HealthBar({
  segments,
  className,
}: {
  segments: Array<{ value: number; tone: Tone; label?: string }>;
  className?: string;
}) {
  const total = Math.max(1, segments.reduce((s, x) => s + x.value, 0));
  const bg: Record<Tone, string> = {
    default: "bg-canvas-raised",
    ok:      "bg-status-ok-dot",
    flight:  "bg-status-flight-dot",
    warn:    "bg-status-warn-dot",
    fail:    "bg-status-fail-dot",
    idle:    "bg-canvas-raised",
  };
  return (
    <div className={cn("flex h-2 rounded-full overflow-hidden bg-canvas-raised", className)}>
      {segments.map((s, i) => (
        <div
          key={i}
          className={cn(bg[s.tone], "transition-all duration-300")}
          style={{ width: `${(s.value / total) * 100}%` }}
          title={s.label}
        />
      ))}
    </div>
  );
}

export function Legend({ tone, label, value }: { tone: Tone; label: string; value: number | string }) {
  const dot: Record<Tone, string> = {
    default: "bg-ink-muted",
    ok:      "bg-status-ok-dot",
    flight:  "bg-status-flight-dot",
    warn:    "bg-status-warn-dot",
    fail:    "bg-status-fail-dot",
    idle:    "bg-canvas-raised border border-line",
  };
  return (
    <span className="inline-flex items-center gap-1.5 text-[12px] text-ink-muted">
      <span className={cn("size-1.5 rounded-full", dot[tone])} />
      <span className="text-ink-DEFAULT tabular-nums">{value}</span>
      <span>{label}</span>
    </span>
  );
}
