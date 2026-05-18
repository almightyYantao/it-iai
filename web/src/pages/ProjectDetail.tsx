import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { Breadcrumbs, PageContainer, PageHeader } from "../components/Page";
import { StatusBadge, VisibilityPill } from "../components/StatusBadge";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { CollaboratorsPanel } from "../components/CollaboratorsPanel";
import { DomainsPanel } from "../components/DomainsPanel";
import { EnvPanel } from "../components/EnvPanel";
import { ProjectAccessPanel } from "../components/ProjectAccessPanel";
import { ExternalIcon, LayersIcon } from "../components/icons";
import { shortID, timeAgo } from "../lib/format";

export function ProjectDetail() {
  const { t } = useI18n();
  const { slug = "" } = useParams();
  const nav = useNavigate();
  const [pg, setPg] = useState<PaginationState>({ page: 1, pageSize: 10 });

  const project = useQuery({
    queryKey: ["project", slug],
    queryFn: () => api.getProject(slug),
    refetchInterval: 5000,
  });
  const who = useQuery({ queryKey: ["whoami"], queryFn: api.whoami, retry: false });
  const deployments = useQuery({
    queryKey: ["deployments", slug, pg.page, pg.pageSize],
    queryFn: () => api.listDeployments(slug, pg.pageSize, (pg.page - 1) * pg.pageSize),
    refetchInterval: 5000,
  });

  if (project.error) {
    return (
      <PageContainer>
        <PageHeader eyebrow={t("nav.projects")} title={slug} />
        <ErrorState message={(project.error as Error).message} onRetry={() => project.refetch()} />
      </PageContainer>
    );
  }

  if (!project.data) {
    return (
      <PageContainer>
        <PageHeader eyebrow={t("nav.projects")} title={<span className="text-ink-muted">{t("common.loading")}</span>} />
        <LoadingRows rows={4} cols={4} />
      </PageContainer>
    );
  }

  const p = project.data.project;
  const pod = project.data.pod;
  const list = deployments.data?.deployments ?? [];

  // "Manage" rights mirror the backend: project owner OR an admin user OR a
  // platform-wide deploy token. Collaborators (editor) can push but not
  // change access policy or delete.
  const canManage = (() => {
    if (!who.data) return false;
    if (who.data.kind === "user") {
      return who.data.admin === true || who.data.email?.toLowerCase() === p.owner_email?.toLowerCase();
    }
    // Token actors: platform-wide token (no project_id) acts as admin.
    return !who.data.project_id;
  })();

  return (
    <PageContainer>
      <PageHeader
        eyebrow={
          <Breadcrumbs
            items={[
              { label: t("nav.projects"), to: "/projects" },
              { label: p.slug, current: true },
            ]}
          />
        }
        title={p.name || p.slug}
        description={
          <span className="flex flex-wrap items-center gap-3">
            <StatusBadge status={p.status} />
            <VisibilityPill kind={p.visibility} />
            <span className="text-ink-faint">·</span>
            <span>{t("project.lastpushed", { when: timeAgo(p.last_pushed_at) })}</span>
          </span>
        }
        actions={
          <a
            href={p.url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 rounded-md border border-line bg-canvas-surface hover:bg-canvas-raised px-3 py-1.5 text-[12.5px] font-medium font-mono text-ink-DEFAULT transition"
          >
            {p.url.replace(/^https?:\/\//, "")}
            <ExternalIcon className="size-3.5 opacity-70" />
          </a>
        }
      />

      {pod && (
        <section className="mb-6 grid grid-cols-2 sm:grid-cols-4 gap-x-6 gap-y-3 rounded-lg border border-line-subtle bg-canvas-surface px-5 py-4">
          <PodStat label={t("project.pod.node")} value={pod.node || "—"} mono />
          <PodStat label={t("project.pod.phase")} value={pod.ready ? `${pod.phase} · Ready` : pod.phase || "—"} />
          <PodStat label={t("project.pod.podip")} value={pod.pod_ip || "—"} mono />
          <PodStat label={t("project.pod.name")} value={pod.pod || "—"} mono />
        </section>
      )}

      <section className="mb-3 flex items-end justify-between">
        <h2 className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">{t("project.recent")}</h2>
        <span className="text-[11.5px] text-ink-faint">{t("project.shown", { count: list.length })}</span>
      </section>

      {deployments.isLoading && <LoadingRows rows={3} cols={4} />}

      {deployments.data && list.length === 0 && (
        <EmptyState
          icon={<LayersIcon className="size-7" />}
          title={t("project.deployments.empty.title")}
          description={
            <span
              dangerouslySetInnerHTML={{
                __html: t("project.deployments.empty.description", {
                  cmd: '<code class="font-mono text-ink-DEFAULT">deploy +push</code>',
                }),
              }}
            />
          }
        />
      )}

      {list.length > 0 && (
        <>
          <div className="overflow-hidden rounded-lg border border-line-subtle bg-canvas-surface">
            <table className="min-w-full text-[13px]">
              <thead className="bg-canvas-base/40">
                <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                  <th className="px-5 py-3 font-medium">{t("project.deployments.col.id")}</th>
                  <th className="px-5 py-3 font-medium">{t("project.deployments.col.status")}</th>
                  <th className="px-5 py-3 font-medium">{t("project.deployments.col.trigger")}</th>
                  <th className="px-5 py-3 font-medium">{t("project.deployments.col.image")}</th>
                  <th className="px-5 py-3 font-medium">{t("project.deployments.col.started")}</th>
                  <th className="px-5 py-3 font-medium">{t("project.deployments.col.deployed")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line-subtle/60">
                {list.map((d) => (
                  <tr
                    key={d.id}
                    onClick={() => nav(`/projects/${slug}/deployments/${d.id}`)}
                    className="group cursor-pointer hover:bg-canvas-raised/50 transition-colors"
                  >
                    <td className="px-5 py-3.5 font-mono text-[12px]">
                      <Link
                        to={`/projects/${slug}/deployments/${d.id}`}
                        onClick={(e) => e.stopPropagation()}
                        className="text-ink-DEFAULT group-hover:text-brand"
                      >
                        {shortID(d.id)}
                      </Link>
                    </td>
                    <td className="px-5 py-3.5">
                      <StatusBadge status={d.status} />
                    </td>
                    <td className="px-5 py-3.5 text-ink-muted">{d.trigger_type}</td>
                    <td className="px-5 py-3.5 font-mono text-[11.5px] text-ink-faint max-w-xs truncate">
                      {d.image_tag || t("common.empty")}
                    </td>
                    <td className="px-5 py-3.5 text-ink-muted tabular-nums">{timeAgo(d.build_started_at)}</td>
                    <td className="px-5 py-3.5 text-ink-muted tabular-nums">{timeAgo(d.deployed_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination state={pg} total={deployments.data?.total ?? 0} onChange={setPg} />
        </>
      )}

      <div className="mt-10">
        <CollaboratorsPanel slug={slug} ownerEmail={p.owner_email} />
      </div>

      <div className="mt-8">
        <DomainsPanel slug={slug} canEdit={canManage} />
      </div>

      <div className="mt-8">
        <EnvPanel slug={slug} canEdit={canManage} />
      </div>

      <div className="mt-8">
        <ProjectAccessPanel
          slug={slug}
          initialPreset={p.access_preset ?? null}
          initialCIDRs={p.allow_cidrs ?? []}
          visibility={p.visibility}
          canEdit={canManage}
          // Only admins (or platform tokens) can edit the global preset
          // list — mirror the backend permission so non-admins don't get a
          // dead-link "Manage presets →".
          canManagePresets={
            who.data?.kind === "user"
              ? Boolean(who.data.admin)
              : !who.data?.project_id
          }
        />
      </div>

      {canManage && (
        <div className="mt-10">
          <DangerZone slug={slug} name={p.name || p.slug} onDeleted={() => nav("/projects", { replace: true })} />
        </div>
      )}
    </PageContainer>
  );
}

function DangerZone({
  slug,
  name,
  onDeleted,
}: {
  slug: string;
  name: string;
  onDeleted: () => void;
}) {
  const { t } = useI18n();
  const [confirmText, setConfirmText] = useState("");
  const [opError, setOpError] = useState<string | null>(null);

  const del = useMutation({
    mutationFn: () => api.deleteProject(slug),
    onSuccess: () => onDeleted(),
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  // Require the user to type the slug to confirm — same pattern as Vercel /
  // GitHub. Prevents misclicks; cheaper than a multi-step modal.
  const armed = confirmText.trim() === slug;

  return (
    <section className="rounded-lg border border-status-fail-fg/30 bg-status-fail-bg/20">
      <header className="px-5 py-4 border-b border-status-fail-fg/20">
        <h3 className="text-[14px] font-semibold text-status-fail-fg">{t("danger.title")}</h3>
        <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">
          {t("danger.delete.desc", { name })}
        </p>
      </header>
      <div className="px-5 py-4 grid grid-cols-1 md:grid-cols-[1fr_auto] gap-3 items-end">
        <div>
          <label className="block text-[11.5px] text-ink-muted mb-1.5">
            {t("danger.delete.confirm.label", { slug })}
          </label>
          <input
            type="text"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder={slug}
            spellCheck={false}
            autoComplete="off"
            className="w-full rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-2 focus:ring-status-fail-fg/30 focus:border-status-fail-fg"
          />
        </div>
        <button
          onClick={() => del.mutate()}
          disabled={!armed || del.isPending}
          className={cn(
            "shrink-0 px-4 py-1.5 rounded-md text-[12.5px] font-medium transition-colors",
            armed && !del.isPending
              ? "bg-status-fail-fg text-canvas-base hover:opacity-90"
              : "bg-canvas-raised text-ink-faint cursor-not-allowed",
          )}
        >
          {del.isPending ? t("danger.delete.button.busy") : t("danger.delete.button")}
        </button>
      </div>
      {opError && (
        <div className="px-5 pb-4">
          <div className="rounded-md border border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg text-[12px] px-3 py-2">
            {opError}
          </div>
        </div>
      )}
    </section>
  );
}

function PodStat({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-[10.5px] uppercase tracking-[0.12em] text-ink-faint font-medium mb-1">{label}</div>
      <div className={`text-[12.5px] text-ink-DEFAULT truncate ${mono ? "font-mono" : ""}`} title={value}>
        {value}
      </div>
    </div>
  );
}
