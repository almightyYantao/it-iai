import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { PageContainer, PageHeader, SegmentedControl } from "../components/Page";
import { StatusBadge, VisibilityPill } from "../components/StatusBadge";
import { OwnerCell } from "../components/Owner";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { ExternalIcon, LayersIcon } from "../components/icons";
import { timeAgo } from "../lib/format";

type Scope = "all" | "mine";

export function Projects() {
  const { t } = useI18n();
  const nav = useNavigate();

  const [scope, setScope] = useState<Scope>("all");
  const [pg, setPg] = useState<PaginationState>({ page: 1, pageSize: 10 });

  // Reset to page 1 when scope toggles — different result set.
  useEffect(() => {
    setPg((s) => ({ ...s, page: 1 }));
  }, [scope]);

  const q = useQuery({
    queryKey: ["projects", scope, pg.page, pg.pageSize],
    queryFn: () => {
      const offset = (pg.page - 1) * pg.pageSize;
      return scope === "all"
        ? api.listAllProjects(pg.pageSize, offset)
        : api.listMyProjects(pg.pageSize, offset);
    },
    refetchInterval: 5000,
  });

  return (
    <PageContainer>
      <PageHeader
        eyebrow={t("projects.eyebrow")}
        title={t("projects.title")}
        description={scope === "all" ? t("projects.description.all") : t("projects.description.mine")}
        actions={
          <SegmentedControl
            value={scope}
            onChange={setScope}
            options={[
              { value: "mine", label: t("projects.toggle.mine") },
              { value: "all", label: t("projects.toggle.all") },
            ]}
          />
        }
      />

      {q.error && (
        <div className="mb-4">
          <ErrorState
            message={
              q.error instanceof ApiError && q.error.status === 403
                ? t("projects.error.noadmin")
                : (q.error as Error).message
            }
            onRetry={() => q.refetch()}
          />
        </div>
      )}

      {q.isLoading && <LoadingRows rows={4} cols={4} />}

      {q.data && (q.data.projects?.length ?? 0) === 0 && pg.page === 1 && (
        <EmptyState
          icon={<LayersIcon className="size-7" />}
          title={t("projects.empty.title")}
          description={
            <span
              dangerouslySetInnerHTML={{
                __html: t("projects.empty.description", {
                  cmd: '<code class="font-mono text-ink-DEFAULT">deploy +push</code>',
                }),
              }}
            />
          }
        />
      )}

      {q.data && (q.data.projects?.length ?? 0) > 0 && (
        <>
          <div className="overflow-hidden rounded-lg border border-line-subtle bg-canvas-surface">
            <table className="min-w-full text-[13px]">
              <thead className="bg-canvas-base/40">
                <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                  <th className="px-5 py-3 font-medium">{t("projects.col.project")}</th>
                  <th className="px-5 py-3 font-medium">{t("projects.col.owner")}</th>
                  <th className="px-5 py-3 font-medium">{t("projects.col.visibility")}</th>
                  <th className="px-5 py-3 font-medium">{t("projects.col.status")}</th>
                  <th className="px-5 py-3 font-medium">{t("projects.col.lastpush")}</th>
                  <th className="px-5 py-3 font-medium text-right">{t("projects.col.url")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-subtle/60">
                {q.data.projects.map((p) => (
                  <tr
                    key={p.id}
                    onClick={() => nav(`/projects/${p.slug}`)}
                    className="group cursor-pointer hover:bg-canvas-raised/50 transition-colors"
                  >
                    <td className="px-5 py-3.5">
                      <Link
                        to={`/projects/${p.slug}`}
                        onClick={(e) => e.stopPropagation()}
                        className="font-medium text-ink-strong group-hover:text-brand"
                      >
                        {p.slug}
                      </Link>
                      {p.name && p.name !== p.slug && (
                        <div className="text-[12px] text-ink-faint mt-0.5">{p.name}</div>
                      )}
                    </td>
                    <td className="px-5 py-3.5 text-ink-muted">
                      <OwnerCell email={p.owner_email} />
                    </td>
                    <td className="px-5 py-3.5">
                      <VisibilityPill kind={p.visibility} />
                    </td>
                    <td className="px-5 py-3.5">
                      <StatusBadge status={p.status} />
                    </td>
                    <td className="px-5 py-3.5 text-ink-muted tabular-nums">
                      {timeAgo(p.last_pushed_at)}
                    </td>
                    <td className="px-5 py-3.5 text-right">
                      <a
                        href={p.url}
                        onClick={(e) => e.stopPropagation()}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1.5 font-mono text-[12px] text-ink-muted hover:text-brand"
                      >
                        {p.url.replace(/^https?:\/\//, "")}
                        <ExternalIcon className="size-3 opacity-0 group-hover:opacity-60 transition" />
                      </a>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination state={pg} total={q.data.total ?? 0} onChange={setPg} />
        </>
      )}
    </PageContainer>
  );
}
