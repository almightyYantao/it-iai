import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { PageContainer, PageHeader } from "../components/Page";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { ActivityIcon } from "../components/icons";
import { OwnerCell } from "../components/Owner";
import { timeAgo } from "../lib/format";

export function Users() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [pg, setPg] = useState<PaginationState>({ page: 1, pageSize: 10 });

  const q = useQuery({
    queryKey: ["users", pg.page, pg.pageSize],
    queryFn: () => api.listUsers(pg.pageSize, (pg.page - 1) * pg.pageSize),
    refetchInterval: 15000,
  });

  const toggle = useMutation({
    mutationFn: ({ id, isAdmin }: { id: string; isAdmin: boolean }) => api.setUserAdmin(id, isAdmin),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  function onToggle(u: { id: string; email: string; is_admin: boolean }) {
    const next = !u.is_admin;
    const msg = next
      ? t("users.confirm.promote", { email: u.email })
      : t("users.confirm.demote", { email: u.email });
    if (!window.confirm(msg)) return;
    toggle.mutate({ id: u.id, isAdmin: next });
  }

  return (
    <PageContainer>
      <PageHeader
        eyebrow={t("users.eyebrow")}
        title={t("users.title")}
        description={t("users.description")}
      />

      {q.error && (
        <ErrorState
          message={
            q.error instanceof ApiError && q.error.status === 403
              ? t("users.error.noadmin")
              : (q.error as Error).message
          }
          onRetry={() => q.refetch()}
        />
      )}

      {q.isLoading && <LoadingRows rows={4} cols={5} />}

      {q.data && (q.data.users?.length ?? 0) === 0 && pg.page === 1 && (
        <EmptyState
          icon={<ActivityIcon className="size-7" />}
          title={t("users.empty.title")}
          description={t("users.empty.description")}
        />
      )}

      {q.data && (q.data.users?.length ?? 0) > 0 && (
        <>
          <div className="overflow-hidden rounded-lg border border-line-subtle bg-canvas-surface">
            <table className="min-w-full text-[13px]">
              <thead className="bg-canvas-base/40">
                <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                  <th className="px-5 py-3 font-medium">{t("users.col.email")}</th>
                  <th className="px-5 py-3 font-medium">{t("users.col.role")}</th>
                  <th className="px-5 py-3 font-medium">{t("users.col.lastseen")}</th>
                  <th className="px-5 py-3 font-medium">{t("users.col.created")}</th>
                  <th className="px-5 py-3 font-medium text-right">{t("users.col.actions")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-subtle/60">
                {q.data.users.map((u) => (
                  <tr key={u.id} className="hover:bg-canvas-raised/50 transition-colors">
                    <td className="px-5 py-3.5">
                      <OwnerCell email={u.email} />
                      {u.name && <div className="text-[11.5px] text-ink-faint mt-0.5 ml-7">{u.name}</div>}
                    </td>
                    <td className="px-5 py-3.5">
                      {u.is_admin ? (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-status-flight-bg text-status-flight-fg px-2 py-0.5 text-[11.5px] font-medium">
                          {t("users.role.admin")}
                        </span>
                      ) : (
                        <span className="text-ink-muted text-[12px]">{t("users.role.member")}</span>
                      )}
                    </td>
                    <td className="px-5 py-3.5 text-ink-muted tabular-nums">{timeAgo(u.last_seen_at)}</td>
                    <td className="px-5 py-3.5 text-ink-muted tabular-nums">{timeAgo(u.created_at)}</td>
                    <td className="px-5 py-3.5 text-right">
                      <button
                        onClick={() => onToggle(u)}
                        disabled={toggle.isPending}
                        className={
                          "text-[12px] transition-colors " +
                          (u.is_admin
                            ? "text-ink-faint hover:text-status-fail-fg"
                            : "text-ink-faint hover:text-brand")
                        }
                      >
                        {u.is_admin ? t("users.action.demote") : t("users.action.promote")}
                      </button>
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
