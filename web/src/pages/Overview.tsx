import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { StatusBadge } from "../components/StatusBadge";

import { api } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { PageContainer, PageHeader } from "../components/Page";
import { HealthBar, Legend, Tile } from "../components/Tile";
import { ErrorState, LoadingRows } from "../components/States";
import { Card } from "../components/ui";

export function Overview() {
  const { t } = useI18n();
  const m = useQuery({
    queryKey: ["metrics"],
    queryFn: api.metrics,
    refetchInterval: 5000,
  });
  const ps = useQuery({ queryKey: ["overview-projects"], queryFn: () => api.listAllProjects(200, 0), refetchInterval: 15000 });
  const attention = (ps.data?.projects ?? []).filter((p) => p.status !== "running").slice(0, 5);

  return (
    <PageContainer>
      <PageHeader
        eyebrow={t("overview.eyebrow")}
        title={t("overview.title")}
        description={t("overview.description")}
      />

      {m.isLoading && <LoadingRows rows={3} />}
      {m.error && (
        <ErrorState message={t("overview.error")} onRetry={() => m.refetch()} />
      )}

      {m.data && (
        <div className="grid grid-cols-12 gap-4">
          <Card className="col-span-12 lg:col-span-7 transition-colors hover:border-line px-6 py-5">
            <div className="text-[13px] text-ink-muted font-medium">
              {t("overview.health.label")}
            </div>
            <div className="mt-3 flex items-baseline gap-3">
              <div className="text-[48px] font-semibold tracking-[-0.04em] tabular-nums leading-none text-ink-strong">
                {m.data.projects.running}
              </div>
              <div className="text-[13.5px] text-ink-muted">
                {t("overview.health.summary", {
                  running: m.data.projects.running,
                  total: m.data.projects.total,
                })}
              </div>
            </div>
            <HealthBar
              className="mt-5"
              segments={[
                { value: m.data.projects.running, tone: "ok" },
                { value: m.data.projects.failed, tone: "fail" },
                {
                  value: Math.max(
                    0,
                    m.data.projects.total - m.data.projects.running - m.data.projects.failed,
                  ),
                  tone: "idle",
                },
              ]}
            />
            <div className="mt-3 flex flex-wrap gap-x-5 gap-y-2">
              <Legend tone="ok" label={t("overview.health.running")} value={m.data.projects.running} />
              <Legend tone="fail" label={t("overview.health.errored")} value={m.data.projects.failed} />
              <Legend
                tone="idle"
                label={t("overview.health.idle")}
                value={Math.max(
                  0,
                  m.data.projects.total - m.data.projects.running - m.data.projects.failed,
                )}
              />
            </div>
            <div className="mt-6 border-t border-line-subtle pt-4">
              <div className="mb-2 text-[13px] font-medium text-ink-strong">{t("overview.attention")}</div>
              {attention.length === 0 ? (
                <p className="text-[12.5px] text-ink-faint">{t("overview.attention.none")}</p>
              ) : (
                <ul className="divide-y divide-line-subtle">
                  {attention.map((p) => (
                    <li key={p.id}>
                      <Link to={`/projects/${p.slug}`} className="flex items-center justify-between gap-3 py-2 text-[13px] hover:text-ink-strong">
                        <span className="truncate text-ink">{p.name || p.slug}</span>
                        <StatusBadge status={p.status} />
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </Card>

          <div className="col-span-12 lg:col-span-5 grid grid-rows-3 gap-4">
            <Tile
              label={t("overview.tile.inflight")}
              value={m.data.deployments.in_flight}
              tone={m.data.deployments.in_flight > 0 ? "flight" : "idle"}
              hint={
                m.data.deployments.in_flight > 0
                  ? t("overview.tile.inflight.hint.active")
                  : t("overview.tile.inflight.hint.idle")
              }
            />
            <Tile label={t("overview.tile.deployments24h")} value={m.data.deployments.last_24h} />
            <Tile
              label={t("overview.tile.failures24h")}
              value={m.data.deployments.failed_last_24h}
              tone={m.data.deployments.failed_last_24h > 0 ? "fail" : "idle"}
            />
          </div>
        </div>
      )}
    </PageContainer>
  );
}
