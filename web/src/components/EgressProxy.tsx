import { confirmDialog } from "./ui/confirm";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { Badge, Button, Card } from "./ui";
import { timeAgo } from "../lib/format";
import type { EgressMode } from "../lib/types";

// Shared query so the nav dot and the status card poll once.
export function useProxyStatus(enabled = true) {
  return useQuery({ queryKey: ["egress-proxy-status"], queryFn: api.egressProxyStatus, refetchInterval: 10000, enabled, retry: false });
}

export function ProxyDot() {
  const { t } = useI18n();
  const q = useProxyStatus();
  if (!q.data) return null;
  const p = q.data.proxy;
  const ok = !!p.ready;
  const tip = ok
    ? t("ep.dot.ok")
    : `${t("ep.dot.bad")}: ${p.reason || p.phase}${p.restarts ? ` · ${t("ep.restarts", { n: String(p.restarts) })}` : ""}${p.last_error ? ` · ${p.last_error}` : ""}`;
  return (
    <span title={tip} className="inline-flex items-center gap-1.5 rounded-md border border-line-subtle px-2 py-1 text-[12px] text-ink-muted">
      <span className={cn("size-2 rounded-full", ok ? "bg-status-ok-dot" : "bg-status-fail-dot animate-pulse")} />
      {t("ep.dot.label")}
    </span>
  );
}

export function ProxyStatusCard() {
  const { t } = useI18n();
  const q = useProxyStatus();
  const d = q.data;
  const p = d?.proxy;
  const ok = !!p?.ready;
  const proxied = (d?.modes.observe ?? 0) + (d?.modes.enforce ?? 0);
  return (
    <Card className="mb-8 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <span className={cn("size-2.5 rounded-full", !d ? "bg-ink-faint" : ok ? "bg-status-ok-dot" : "bg-status-fail-dot animate-pulse")} />
          <h2 className="text-[15px] font-semibold text-ink-strong">{t("ep.title")}</h2>
          <Badge variant={ok ? "success" : "danger"}>
            {!d ? "…" : ok ? t("ep.running") : p?.reason || p?.phase}
          </Badge>
        </div>
        {d && <span className="text-[12px] text-ink-faint">{t("ep.checked", { ago: timeAgo(d.checked_at) })}</span>}
      </div>
      {q.error && <p className="mt-3 text-[13px] text-status-fail-fg">{(q.error as Error).message}</p>}
      {d && (
        <div className="mt-4 grid grid-cols-2 gap-4 md:grid-cols-5">
          <Stat label={t("ep.uptime")} value={p?.started_at ? timeAgo(p.started_at).replace(/前$| ago$/, "") : "—"} />
          <Stat label={t("ep.restarts.label")} value={p?.restarts ?? 0} bad={(p?.restarts ?? 0) > 0 && !ok} />
          <Stat label={t("ep.projects")} value={`${proxied} (${t("ep.enforce")} ${d.modes.enforce ?? 0})`} />
          <Link to="/egress-logs?status=allowed" className="hover:opacity-70"><Stat label={t("ep.allowed") + " →"} value={d.recent?.allowed ?? 0} /></Link>
          <Link to="/egress-logs?status=denied" className="hover:opacity-70"><Stat label={t("ep.denied") + " →"} value={d.recent?.denied ?? 0} bad={(d.recent?.denied ?? 0) > 0} /></Link>
        </div>
      )}
      {p?.last_error && !ok && <p className="mt-3 font-mono text-[12px] text-status-fail-fg">{t("ep.lasterr")}: {p.last_error}</p>}
      {p?.config_hash && <p className="mt-3 font-mono text-[11.5px] text-ink-faint">config {p.config_hash}</p>}
    </Card>
  );
}

function Stat({ label, value, bad }: { label: string; value: React.ReactNode; bad?: boolean }) {
  return (
    <div>
      <div className="text-[12px] text-ink-muted">{label}</div>
      <div className={cn("mt-1 text-[18px] font-semibold tabular-nums", bad ? "text-status-fail-fg" : "text-ink-strong")}>{value}</div>
    </div>
  );
}

const MODES: EgressMode[] = ["off", "observe", "enforce"];

export function EgressModePanel({ slug, notify }: { slug: string; notify: (ok: boolean, text: string) => void }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["egress-mode", slug], queryFn: () => api.getEgressMode(slug) });
  const rep = useQuery({ queryKey: ["egress-report", slug], queryFn: () => api.egressReport(slug), enabled: (q.data?.mode ?? "off") !== "off", refetchInterval: 30000 });
  const set = useMutation({
    mutationFn: (m: EgressMode) => api.setEgressMode(slug, m),
    onSuccess: () => {
      notify(true, t("ep.mode.saved"));
      qc.invalidateQueries({ queryKey: ["egress-mode", slug] });
      qc.invalidateQueries({ queryKey: ["egress-proxy-status"] });
    },
    onError: (e) => notify(false, e instanceof ApiError ? e.message : String(e)),
  });
  const cur = q.data?.mode ?? "off";
  async function pick(m: EgressMode) {
    if (m === cur) return;
    if (!(await confirmDialog({ title: t(`ep.mode.${m}`), description: t(`ep.mode.confirm.${m}`), danger: m === "enforce" }))) return;
    set.mutate(m);
  }
  const domains = rep.data?.domains ?? [];
  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between">
        <h4 className="text-[13px] font-semibold text-ink-strong">{t("ep.mode.title")}</h4>
        <span className="text-[11.5px] text-ink-faint">{t("ep.mode.hint")}</span>
      </div>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        {MODES.map((m) => (
          <Button key={m} variant="secondary" disabled={set.isPending || !q.data?.can_edit} onClick={() => pick(m)}
            className={cn("block h-auto px-3 py-2.5 text-left font-normal whitespace-normal disabled:opacity-60",
              cur === m ? "border-ink-strong bg-canvas-surface" : "border-line-subtle hover:border-line")}>
            <div className="text-[12.5px] font-medium text-ink-strong">{t(`ep.mode.${m}`)}</div>
            <div className="mt-0.5 text-[11px] text-ink-muted">{t(`ep.mode.${m}.hint`)}</div>
          </Button>
        ))}
      </div>
      {cur !== "off" && (
        <div className="mt-3 rounded-md border border-line-subtle">
          <div className="flex justify-between border-b border-line-subtle bg-canvas-surface px-3 py-2 text-[12px] font-medium text-ink-muted"><span>{t("ep.report")}</span><Link className="hover:text-ink-strong" to={`/egress-logs?project=${slug}`}>{t("ep.report.all")} →</Link></div>
          {domains.length === 0 ? (
            <p className="px-3 py-4 text-[12.5px] text-ink-faint">{t("ep.report.empty")}</p>
          ) : (
            <ul className="divide-y divide-line-subtle">
              {domains.map((d) => (
                <li key={d.host} className="flex items-center justify-between gap-3 px-3 py-2 text-[12.5px]">
                  <code className="truncate font-mono text-ink-strong">{d.host}</code>
                  <span className="shrink-0 tabular-nums text-ink-muted">
                    <span className="mr-3 text-ink-faint">{timeAgo(d.last_seen)}</span>
                    {d.allowed} ✓{d.denied > 0 && <span className="ml-2 text-status-fail-fg">{d.denied} ✕</span>}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
