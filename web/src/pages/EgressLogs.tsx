import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";

import { api } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";
import { PageContainer, PageHeader, SegmentedControl } from "../components/Page";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { input, th } from "../components/ui";

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
        <select className={cn(input, "w-64")} value={project} onChange={(e) => setProject(e.target.value)}>
          <option value="">{t("el.allprojects")}</option>
          {(projects.data?.projects ?? []).map((p) => (
            <option key={p.slug} value={p.slug}>{p.name} ({p.slug})</option>
          ))}
        </select>
        <input className={cn(input, "w-64")} placeholder={t("el.search")} value={q} onChange={(e) => setQ(e.target.value)} />
        <SegmentedControl value={status} onChange={setStatus}
          options={[{ value: "all", label: t("el.all") }, { value: "allowed", label: t("el.allowed") }, { value: "denied", label: t("el.denied") }]} />
        <span className="ml-auto text-[12.5px] text-ink-muted tabular-nums">{t("el.total", { n: String(logs.data?.total ?? 0) })}</span>
      </div>

      {logs.error && <ErrorState message={(logs.error as Error).message} onRetry={() => logs.refetch()} />}
      {logs.isLoading && <LoadingRows rows={6} cols={4} />}
      {logs.data && logs.data.entries.length === 0 && <EmptyState title={t("el.empty")} description={t("el.empty.desc")} />}
      {logs.data && logs.data.entries.length > 0 && (
        <>
          <div className="overflow-x-auto rounded-lg border border-line-subtle">
            <table className="min-w-full text-[13px]">
              <thead className="bg-canvas-surface">
                <tr>
                  <th className={th}>{t("el.col.time")}</th>
                  <th className={th}>{t("el.col.project")}</th>
                  <th className={th}>{t("el.col.method")}</th>
                  <th className={th}>{t("el.col.host")}</th>
                  <th className={th}>{t("el.col.result")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-subtle">
                {logs.data.entries.map((e) => (
                  <tr key={e.id} className="hover:bg-canvas-surface">
                    <td className="whitespace-nowrap px-4 py-2 text-ink-muted tabular-nums" title={new Date(e.ts).toLocaleString()}>
                      {new Date(e.ts).toLocaleString()} <span className="ml-1 text-ink-faint">· {timeAgo(e.ts)}</span>
                    </td>
                    <td className="px-4 py-2">
                      <button className="text-ink-strong hover:underline" onClick={() => setProject(e.slug)}>{e.slug}</button>
                    </td>
                    <td className="px-4 py-2 font-mono text-[12px] text-ink-muted">{e.method}</td>
                    <td className="px-4 py-2 font-mono text-[12.5px]">
                      <button className="text-ink-strong hover:underline" onClick={() => setQ(e.host)}>{e.host}</button>
                    </td>
                    <td className="px-4 py-2">
                      <span title={e.status} className={cn("rounded-full px-2 py-0.5 text-[11.5px] font-medium",
                        e.denied ? "bg-status-fail-bg text-status-fail-fg" : "bg-status-ok-bg text-status-ok-fg")}>
                        {e.denied ? t("el.denied") : t("el.allowed")} · {e.status.split("/")[1] ?? e.status}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination state={pg} total={logs.data.total} onChange={setPg} pageSizeOptions={[50, 100, 200]} />
        </>
      )}
    </PageContainer>
  );
}
