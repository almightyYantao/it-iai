import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { PageContainer, PageHeader } from "../components/Page";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { ScrollIcon } from "../components/icons";
import { timeAgo } from "../lib/format";

export function Audit() {
  const { t } = useI18n();
  const [pg, setPg] = useState<PaginationState>({ page: 1, pageSize: 10 });

  const q = useQuery({
    queryKey: ["audit", pg.page, pg.pageSize],
    queryFn: () => api.audit(pg.pageSize, (pg.page - 1) * pg.pageSize),
    refetchInterval: 10000,
  });

  return (
    <PageContainer>
      <PageHeader
        eyebrow={t("audit.eyebrow")}
        title={t("audit.title")}
        description={t("audit.description")}
      />

      {q.error && (
        <ErrorState
          message={
            q.error instanceof ApiError && q.error.status === 403
              ? t("audit.error.noadmin")
              : (q.error as Error).message
          }
          onRetry={() => q.refetch()}
        />
      )}

      {q.isLoading && <LoadingRows rows={5} cols={5} />}

      {q.data && (q.data.entries?.length ?? 0) === 0 && pg.page === 1 && (
        <EmptyState
          icon={<ScrollIcon className="size-7" />}
          title={t("audit.empty.title")}
          description={t("audit.empty.description")}
        />
      )}

      {q.data && (q.data.entries?.length ?? 0) > 0 && (
        <>
          <div className="overflow-hidden rounded-lg border border-line-subtle bg-canvas-surface">
            <table className="min-w-full text-[13px]">
              <thead className="bg-canvas-base/40">
                <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                  <th className="px-5 py-3 font-medium">{t("audit.col.when")}</th>
                  <th className="px-5 py-3 font-medium">{t("audit.col.actor")}</th>
                  <th className="px-5 py-3 font-medium">{t("audit.col.action")}</th>
                  <th className="px-5 py-3 font-medium">{t("audit.col.project")}</th>
                  <th className="px-5 py-3 font-medium">{t("audit.col.metadata")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-subtle/60">
                {q.data.entries.map((e) => (
                  <tr key={e.id} className="align-top hover:bg-canvas-raised/50 transition-colors">
                    <td className="px-5 py-3.5 whitespace-nowrap text-ink-muted tabular-nums">
                      {timeAgo(e.created_at)}
                    </td>
                    <td className="px-5 py-3.5 whitespace-nowrap">
                      <span className="text-[10.5px] uppercase tracking-[0.12em] text-ink-faint">
                        {e.actor_type}
                      </span>
                      {/* actor_label is who is accountable: the user's email, or for a
                          token the email of whoever issued it. Fall back to the UUID slice
                          when neither resolves (revoked token, deleted user). */}
                      <div className="text-[12.5px] text-ink-DEFAULT">
                        {e.actor_label || e.actor_id.slice(0, 12)}
                      </div>
                      {/* Only set when the token isn't already the label, i.e. we resolved a
                          person and the token is just the path they came through. */}
                      {e.actor_via && (
                        <div className="text-[11px] text-ink-faint">
                          {t("audit.actor.via", { token: e.actor_via })}
                        </div>
                      )}
                    </td>
                    <td className="px-5 py-3.5 font-mono text-[12px] text-status-flight-fg">{e.action}</td>
                    <td className="px-5 py-3.5 font-mono text-[11.5px] text-ink-faint">
                      {e.project_id ? e.project_id.slice(0, 8) : t("common.empty")}
                    </td>
                    <td className="px-5 py-3.5 font-mono text-[11.5px] text-ink-muted max-w-md break-all">
                      {e.metadata || ""}
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
