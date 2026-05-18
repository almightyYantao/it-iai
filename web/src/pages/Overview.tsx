import { useQuery } from "@tanstack/react-query";

import { api } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { PageContainer, PageHeader } from "../components/Page";
import { HealthBar, Legend, Tile } from "../components/Tile";
import { ErrorState, LoadingRows } from "../components/States";

export function Overview() {
  const { t } = useI18n();
  const m = useQuery({
    queryKey: ["metrics"],
    queryFn: api.metrics,
    refetchInterval: 5000,
  });

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
          <div className="col-span-12 lg:col-span-7 rounded-lg border border-line-subtle bg-canvas-surface px-6 py-5">
            <div className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
              {t("overview.health.label")}
            </div>
            <div className="mt-3 flex items-baseline gap-3">
              <div className="text-[44px] font-semibold tabular-nums leading-none text-ink-strong">
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
          </div>

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
