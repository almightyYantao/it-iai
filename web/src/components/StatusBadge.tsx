import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";
import type { DeploymentStatus, ProjectStatus } from "../lib/types";

type Status = DeploymentStatus | ProjectStatus | string;

type Tone = "ok" | "flight" | "warn" | "fail" | "idle";

const toneOf: Record<string, Tone> = {
  running: "ok",
  queued: "flight",
  building: "flight",
  pushing: "flight",
  deploying: "flight",
  pending: "idle",
  created: "idle",
  stopped: "idle",
  superseded: "idle",
  failed: "fail",
  error: "fail",
  deleting: "fail",
};

const toneClass: Record<Tone, string> = {
  ok:     "bg-status-ok-bg text-status-ok-fg",
  flight: "bg-status-flight-bg text-status-flight-fg",
  warn:   "bg-status-warn-bg text-status-warn-fg",
  fail:   "bg-status-fail-bg text-status-fail-fg",
  idle:   "bg-status-idle-bg text-status-idle-fg",
};
const dotClass: Record<Tone, string> = {
  ok:     "bg-status-ok-dot",
  flight: "bg-status-flight-dot",
  warn:   "bg-status-warn-dot",
  fail:   "bg-status-fail-dot",
  idle:   "bg-status-idle-dot",
};

const PULSE = new Set(["building", "pushing", "deploying", "queued"]);

export function StatusBadge({ status, size = "sm" }: { status: Status; size?: "sm" | "md" }) {
  const { t } = useI18n();
  const tone: Tone = toneOf[status] ?? "idle";
  const pulse = PULSE.has(status);
  // Use the status key as i18n key — falls back to the raw status when the
  // key isn't in the dictionary (so future status strings still render).
  const label = t(`status.${status}`);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full font-medium tabular-nums",
        toneClass[tone],
        size === "sm" ? "px-2 py-0.5 text-[11.5px]" : "px-2.5 py-1 text-[12.5px]"
      )}
    >
      <span className="relative flex size-1.5 shrink-0">
        {pulse && <span className={cn("absolute inset-0 rounded-full opacity-70 animate-ping", dotClass[tone])} />}
        <span className={cn("relative size-1.5 rounded-full", dotClass[tone])} />
      </span>
      {label}
    </span>
  );
}

export function VisibilityPill({ kind }: { kind: string }) {
  const { t } = useI18n();
  const label = t(`visibility.${kind.toLowerCase()}`);
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-[11.5px] text-ink-muted bg-canvas-raised/40 border border-line-subtle">
      {label}
    </span>
  );
}
