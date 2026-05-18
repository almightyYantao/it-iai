import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, subscribeEvents } from "../lib/api";
import { useI18n } from "../lib/i18n";
import type { DeploymentEvent } from "../lib/types";
import { Breadcrumbs, PageContainer, PageHeader } from "../components/Page";
import { StatusBadge } from "../components/StatusBadge";
import { ErrorState } from "../components/States";
import { LogViewer } from "../components/LogViewer";
import { shortID, timeAgo } from "../lib/format";

export function DeploymentDetail() {
  const { t } = useI18n();
  const { slug = "", id = "" } = useParams();
  const [events, setEvents] = useState<DeploymentEvent[]>([]);
  const [terminated, setTerminated] = useState<string | null>(null);

  const dep = useQuery({
    queryKey: ["deployment", slug, id],
    queryFn: () => api.getDeployment(slug, id),
    refetchInterval: terminated ? false : 3000,
  });

  useEffect(() => {
    if (!slug || !id) return;
    setEvents([]);
    setTerminated(null);
    const seen = new Set<number>();
    const unsub = subscribeEvents(slug, id, {
      event: (ev) => {
        if (seen.has(ev.id)) return;
        seen.add(ev.id);
        setEvents((prev) => [...prev, ev]);
      },
      end: (status) => setTerminated(status || "ended"),
      error: () => setTerminated("disconnected"),
    });
    return unsub;
  }, [slug, id]);

  if (dep.error) {
    return (
      <PageContainer>
        <PageHeader
          eyebrow={
            <Breadcrumbs items={[
              { label: t("nav.projects"), to: "/projects" },
              { label: slug, to: `/projects/${slug}` },
              { label: shortID(id), current: true },
            ]} />
          }
          title={t("deployment.error.notfound")}
        />
        <ErrorState message={(dep.error as Error).message} />
      </PageContainer>
    );
  }

  const d = dep.data?.deployment;

  return (
    <div className="max-w-[1080px] px-8 pt-10 pb-10">
      <PageHeader
        eyebrow={
          <Breadcrumbs items={[
            { label: t("nav.projects"), to: "/projects" },
            { label: slug, to: `/projects/${slug}` },
            { label: shortID(id), current: true },
          ]} />
        }
        title={
          <>
            {t("deployment.crumb")} <span className="font-mono text-ink-muted text-[20px] ml-1.5">{shortID(id)}</span>
          </>
        }
        description={
          d && (
            <span className="flex flex-wrap items-center gap-3 mt-1">
              <StatusBadge status={d.status} />
              <span className="text-ink-faint">·</span>
              <span>{t("deployment.created", { when: timeAgo(d.created_at) })}</span>
              <span className="text-ink-faint">·</span>
              <span>{t("deployment.via", { trigger: d.trigger_type })}</span>
              {d.deployed_at && (
                <>
                  <span className="text-ink-faint">·</span>
                  <span>{t("deployment.deployed", { when: timeAgo(d.deployed_at) })}</span>
                </>
              )}
            </span>
          )
        }
      />

      {d?.failure_reason && (
        <div className="mb-6 rounded-md bg-status-fail-bg/40 border border-status-fail-fg/25 px-4 py-3 text-[13px] text-status-fail-fg">
          <div className="font-medium mb-1">{t("deployment.failed")}</div>
          <div className="text-status-fail-fg/85 whitespace-pre-wrap break-words">{d.failure_reason}</div>
        </div>
      )}

      <LogViewer events={events} live={!terminated} />

      {terminated && (
        <div className="mt-3 text-[11.5px] text-ink-faint">
          {terminated === "disconnected" ? t("deployment.stream.disconnected") : t("deployment.stream.ended")}
        </div>
      )}
    </div>
  );
}
