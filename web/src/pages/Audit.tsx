import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { PageContainer, PageHeader } from "../components/Page";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { ScrollIcon } from "../components/icons";
import { timeAgo } from "../lib/format";
import { Table, TBody, TD, TH, THead, TR } from "../components/ui";

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
          <Table>
              <THead className="bg-canvas-surface">
                <TR className="text-left text-[12px] font-medium text-ink-muted">
                  <TH className="px-5 py-3 font-medium">{t("audit.col.when")}</TH>
                  <TH className="px-5 py-3 font-medium">{t("audit.col.actor")}</TH>
                  <TH className="px-5 py-3 font-medium">{t("audit.col.action")}</TH>
                  <TH className="px-5 py-3 font-medium">{t("audit.col.project")}</TH>
                  <TH className="px-5 py-3 font-medium">{t("audit.col.metadata")}</TH>
                </TR>
              </THead>
              <TBody className="divide-y divide-line-subtle">
                {q.data.entries.map((e) => (
                  <TR key={e.id} className="align-top hover:bg-canvas-surface transition-colors">
                    <TD className="px-5 py-3.5 whitespace-nowrap text-ink-muted tabular-nums">
                      {timeAgo(e.created_at)}
                    </TD>
                    <TD className="px-5 py-3.5 whitespace-nowrap">
                      <span className="text-[12px] text-ink-faint">
                        {e.actor_type}
                      </span>
                      {/* actor_label is who is accountable: the user's email, or for a
                          token the email of whoever issued it. Fall back to the UUID slice
                          when neither resolves (revoked token, deleted user). */}
                      <div className="text-[12.5px] text-ink">
                        {e.actor_label || e.actor_id.slice(0, 12)}
                      </div>
                      {/* Only set when the token isn't already the label, i.e. we resolved a
                          person and the token is just the path they came through. */}
                      {e.actor_via && (
                        <div className="text-[11px] text-ink-faint">
                          {t("audit.actor.via", { token: e.actor_via })}
                        </div>
                      )}
                    </TD>
                    <TD className="px-5 py-3.5 font-mono text-[12px] text-status-flight-fg">{e.action}</TD>
                    <TD className="px-5 py-3.5 font-mono text-[11.5px] text-ink-faint">
                      {e.project_id ? e.project_id.slice(0, 8) : t("common.empty")}
                    </TD>
                    <TD className="px-5 py-3.5 font-mono text-[11.5px] text-ink-muted max-w-md break-all">
                      {e.metadata || ""}
                    </TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          <Pagination state={pg} total={q.data.total ?? 0} onChange={setPg} />
        </>
      )}
    </PageContainer>
  );
}
