import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";

import { api } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { timeAgo } from "../lib/format";
import { PageContainer, PageHeader } from "../components/Page";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { Badge, Button, Combobox, Input, Table, TBody, TD, TH, THead, TR, Tabs } from "../components/ui";

type Status = "all" | "allowed" | "denied";

export function EgressLogs() {
  const { t } = useI18n();
  const [sp, setSp] = useSearchParams();
  const [project, setProject] = useState(sp.get("project") ?? "");
  const [q, setQ] = useState(sp.get("q") ?? "");
  const [status, setStatus] = useState<Status>((sp.get("status") as Status) || "all");
  const [pg, setPg] = useState<PaginationState>({ page: 1, pageSize: 50 });

  useEffect(() => {
    setPg((s) => ({ ...s, page: 1 }));
    const n = new URLSearchParams();
    if (project) n.set("project", project);
    if (q) n.set("q", q);
    if (status !== "all") n.set("status", status);
    setSp(n, { replace: true });
  }, [project, q, status, setSp]);

  const projects = useQuery({ queryKey: ["admin-projects-all"], queryFn: () => api.listAllProjects(200, 0) });
  const logs = useQuery({
    queryKey: ["egress-logs", project, q, status, pg.page, pg.pageSize],
    queryFn: () => api.egressLogs({ project, q, status, limit: pg.pageSize, offset: (pg.page - 1) * pg.pageSize }),
    refetchInterval: 10000,
  });

  return (
    <PageContainer>
      <PageHeader title={t("el.title")} description={t("el.desc")} />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Combobox className="w-64" value={project} onChange={setProject} placeholder={t("el.allprojects")} allLabel={t("el.allprojects")}
          searchPlaceholder={t("el.searchproject")} empty={t("el.noproject")}
          options={(projects.data?.projects ?? []).map((p) => ({ value: p.slug, label: p.name || p.slug, sub: p.slug }))} />
        <Input className="w-64" placeholder={t("el.search")} value={q} onChange={(e) => setQ(e.target.value)} />
        <Tabs value={status} onChange={setStatus}
          options={[{ value: "all", label: t("el.all") }, { value: "allowed", label: t("el.allowed") }, { value: "denied", label: t("el.denied") }]} />
        <span className="ml-auto text-[12.5px] text-ink-muted tabular-nums">{t("el.total", { n: String(logs.data?.total ?? 0) })}</span>
      </div>

      {logs.error && <ErrorState message={(logs.error as Error).message} onRetry={() => logs.refetch()} />}
      {logs.isLoading && <LoadingRows rows={6} cols={4} />}
      {logs.data && logs.data.entries.length === 0 && <EmptyState title={t("el.empty")} description={t("el.empty.desc")} />}
      {logs.data && logs.data.entries.length > 0 && (
        <>
          <Table>
            <THead>
              <tr>
                <TH>{t("el.col.time")}</TH>
                <TH>{t("el.col.project")}</TH>
                <TH>{t("el.col.method")}</TH>
                <TH>{t("el.col.host")}</TH>
                <TH>{t("el.col.result")}</TH>
              </tr>
            </THead>
            <TBody>
              {logs.data.entries.map((e) => (
                <TR key={e.id}>
                  <TD className="whitespace-nowrap py-2 text-ink-muted tabular-nums" title={new Date(e.ts).toLocaleString()}>
                    {new Date(e.ts).toLocaleString()} <span className="ml-1 text-ink-faint">· {timeAgo(e.ts)}</span>
                  </TD>
                  <TD className="py-2">
                    <Button variant="link" className="font-normal" onClick={() => setProject(e.slug)}>{e.slug}</Button>
                  </TD>
                  <TD className="py-2 font-mono text-[12px] text-ink-muted">{e.method}</TD>
                  <TD className="py-2 font-mono text-[12.5px]">
                    <Button variant="link" className="font-normal font-mono text-[12.5px]" onClick={() => setQ(e.host)}>{e.host}</Button>
                  </TD>
                  <TD className="py-2">
                    <Badge title={e.status} variant={e.denied ? "danger" : "success"}>
                      {e.denied ? t("el.denied") : t("el.allowed")} · {e.status.split("/")[1] ?? e.status}
                    </Badge>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
          <Pagination state={pg} total={logs.data.total} onChange={setPg} pageSizeOptions={[50, 100, 200]} />
        </>
      )}
    </PageContainer>
  );
}
