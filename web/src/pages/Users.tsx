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
import { Badge, Button, confirmDialog, Table, TBody, TD, TH, THead, TR } from "../components/ui";

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

  async function onToggle(u: { id: string; email: string; is_admin: boolean }) {
    const next = !u.is_admin;
    const msg = next
      ? t("users.confirm.promote", { email: u.email })
      : t("users.confirm.demote", { email: u.email });
    if (!(await confirmDialog({ title: msg }))) return;
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
          <Table>
              <THead className="bg-canvas-surface">
                <TR className="text-left text-[12px] font-medium text-ink-muted">
                  <TH className="px-5 py-3 font-medium">{t("users.col.email")}</TH>
                  <TH className="px-5 py-3 font-medium">{t("users.col.role")}</TH>
                  <TH className="px-5 py-3 font-medium">{t("users.col.lastseen")}</TH>
                  <TH className="px-5 py-3 font-medium">{t("users.col.created")}</TH>
                  <TH className="px-5 py-3 font-medium text-right">{t("users.col.actions")}</TH>
                </TR>
              </THead>
              <TBody className="divide-y divide-line-subtle">
                {q.data.users.map((u) => (
                  <TR key={u.id} className="hover:bg-canvas-surface transition-colors">
                    <TD className="px-5 py-3.5">
                      <OwnerCell email={u.email} />
                      {u.name && <div className="text-[11.5px] text-ink-faint mt-0.5 ml-7">{u.name}</div>}
                    </TD>
                    <TD className="px-5 py-3.5">
                      {u.is_admin ? (
                        <Badge variant="info">{t("users.role.admin")}</Badge>
                      ) : (
                        <span className="text-ink-muted text-[12px]">{t("users.role.member")}</span>
                      )}
                    </TD>
                    <TD className="px-5 py-3.5 text-ink-muted tabular-nums">{timeAgo(u.last_seen_at)}</TD>
                    <TD className="px-5 py-3.5 text-ink-muted tabular-nums">{timeAgo(u.created_at)}</TD>
                    <TD className="px-5 py-3.5 text-right">
                      <Button
                        size="sm"
                        variant={u.is_admin ? "destructive-ghost" : "ghost"}
                        onClick={() => onToggle(u)}
                        disabled={toggle.isPending}
                      >
                        {u.is_admin ? t("users.action.demote") : t("users.action.promote")}
                      </Button>
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
