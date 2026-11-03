import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";
import { Badge } from "./ui";
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

const toneVariant: Record<Tone, "success" | "info" | "warning" | "danger" | "default"> = {
  ok: "success",
  flight: "info",
  warn: "warning",
  fail: "danger",
  idle: "default",
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
    <Badge
      variant={toneVariant[tone]}
      className={cn(
        "gap-1.5 tabular-nums",
        tone === "idle" && "bg-status-idle-bg text-status-idle-fg",
        size === "md" && "px-2.5 py-1 text-[12.5px]"
      )}
    >
      <span className="relative flex size-1.5 shrink-0">
        {pulse && <span className={cn("absolute inset-0 rounded-full opacity-70 animate-ping", dotClass[tone])} />}
        <span className={cn("relative size-1.5 rounded-full", dotClass[tone])} />
      </span>
      {label}
    </Badge>
  );
}

export function VisibilityPill({ kind }: { kind: string }) {
  const { t } = useI18n();
  const label = t(`visibility.${kind.toLowerCase()}`);
  return (
    <Badge className="gap-1.5">{label}</Badge>
  );
}
