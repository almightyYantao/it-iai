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
import { Card, Input } from "../components/ui";

type Scope = "all" | "mine";

export function Projects() {
  const { t } = useI18n();
  const nav = useNavigate();

  const [scope, setScope] = useState<Scope>("all");
  const [pg, setPg] = useState<PaginationState>({ page: 1, pageSize: 10 });
  const [search, setSearch] = useState("");
  const [kw, setKw] = useState("");

  // Debounce typing so each keystroke doesn't hit the API.
  useEffect(() => {
    const id = window.setTimeout(() => setKw(search.trim()), 250);
    return () => window.clearTimeout(id);
  }, [search]);

  // Reset to page 1 when scope or search changes — different result set.
  useEffect(() => {
    setPg((s) => ({ ...s, page: 1 }));
  }, [scope, kw]);

  const q = useQuery({
    queryKey: ["projects", scope, kw, pg.page, pg.pageSize],
    queryFn: () => {
      const offset = (pg.page - 1) * pg.pageSize;
      return scope === "all"
        ? api.listAllProjects(pg.pageSize, offset, kw)
        : api.listMyProjects(pg.pageSize, offset, kw);
    },
    placeholderData: (prev) => prev,
    refetchInterval: 5000,
  });

  return (
    <PageContainer>
      <PageHeader
        eyebrow={t("projects.eyebrow")}
        title={t("projects.title")}
        description={scope === "all" ? t("projects.description.all") : t("projects.description.mine")}
        actions={
          <>
          <div className="relative">
            <svg className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-ink-faint" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8">
              <circle cx="9" cy="9" r="6" /><path d="m14 14 4 4" strokeLinecap="round" />
            </svg>
            <Input className="w-64 pl-8" placeholder={t("projects.search")} value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => e.key === "Escape" && setSearch("")} />
          </div>
          <SegmentedControl
            value={scope}
            onChange={setScope}
            options={[
              { value: "mine", label: t("projects.toggle.mine") },
              { value: "all", label: t("projects.toggle.all") },
            ]}
          />
          </>
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

      {q.data && (q.data.projects?.length ?? 0) === 0 && pg.page === 1 && kw && (
        <EmptyState icon={<LayersIcon className="size-7" />} title={t("projects.search.empty", { q: kw })} />
      )}

      {q.data && (q.data.projects?.length ?? 0) === 0 && pg.page === 1 && !kw && (
        <EmptyState
          icon={<LayersIcon className="size-7" />}
          title={t("projects.empty.title")}
          description={
            <span
              dangerouslySetInnerHTML={{
                __html: t("projects.empty.description", {
                  cmd: '<code class="font-mono text-ink">deploy +push</code>',
                }),
              }}
            />
          }
        />
      )}

      {q.data && (q.data.projects?.length ?? 0) > 0 && (
        <>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {q.data.projects.map((p) => (
              <Card
                key={p.id}
                onClick={() => nav(`/projects/${p.slug}`)}
                className="group cursor-pointer p-5 transition-all hover:border-line-strong hover:shadow-raised"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-3">
                    <div className="flex size-8 shrink-0 items-center justify-center rounded-full border border-line-subtle bg-canvas-surface text-[12px] font-semibold uppercase text-ink-strong">
                      {(p.name || p.slug).slice(0, 1)}
                    </div>
                    <div className="min-w-0">
                      <Link
                        to={`/projects/${p.slug}`}
                        onClick={(e) => e.stopPropagation()}
                        className="block truncate text-[14px] font-medium text-ink-strong"
                      >
                        {p.name || p.slug}
                      </Link>
                      <a
                        href={p.url}
                        onClick={(e) => e.stopPropagation()}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex max-w-full items-center gap-1 truncate text-[12.5px] text-ink-muted hover:text-ink-strong hover:underline"
                      >
                        {p.url.replace(/^https?:\/\//, "")}
                        <ExternalIcon className="size-3 shrink-0 opacity-0 transition group-hover:opacity-60" />
                      </a>
                    </div>
                  </div>
                  <VisibilityPill kind={p.visibility} />
                </div>
                <div className="mt-5 flex items-center justify-between gap-3 text-[12.5px] text-ink-muted">
                  <StatusBadge status={p.status} />
                  <span className="tabular-nums">{timeAgo(p.last_pushed_at)}</span>
                </div>
                <div className="mt-3 border-t border-line-subtle pt-3 text-[12px] text-ink-faint">
                  <OwnerCell email={p.owner_email} />
                </div>
              </Card>
            ))}
          </div>
          <Pagination state={pg} total={q.data.total ?? 0} onChange={setPg} />
        </>
      )}
    </PageContainer>
  );
}
