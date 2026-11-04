import { RuntimePanel } from "../components/RuntimePanel";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { Alert, Button, Card, CardBody, CardHeader, Input, Label, Table, TBody, TD, TH, THead, TR } from "../components/ui";
import { Breadcrumbs, PageContainer, PageHeader } from "../components/Page";
import { StatusBadge, VisibilityPill } from "../components/StatusBadge";
import { EmptyState, ErrorState, LoadingRows } from "../components/States";
import { Pagination, type PaginationState } from "../components/Pagination";
import { CollaboratorsPanel } from "../components/CollaboratorsPanel";
import { DomainsPanel } from "../components/DomainsPanel";
import { EnvPanel } from "../components/EnvPanel";
// ProjectAccessPanel is intentionally not mounted — see the commented-out
// render below. noUnusedLocals would fail the build on a dangling import.
// import { ProjectAccessPanel } from "../components/ProjectAccessPanel";
import { ProjectNamePanel } from "../components/ProjectNamePanel";
import { ProjectTLSPanel } from "../components/ProjectTLSPanel";
import { ProjectAPIAccessPanel } from "../components/ProjectAPIAccessPanel";
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
          <Button asChild variant="secondary" className="font-mono">
            <a href={p.url} target="_blank" rel="noreferrer">
              {p.url.replace(/^https?:\/\//, "")}
              <ExternalIcon className="size-3.5 opacity-70" />
            </a>
          </Button>
        }
      />

      {pod && (
        <Card className="mb-6 grid grid-cols-2 sm:grid-cols-4 gap-x-6 gap-y-3 px-5 py-4">
          <PodStat label={t("project.pod.node")} value={pod.node || "—"} mono />
          <PodStat label={t("project.pod.phase")} value={pod.ready ? `${pod.phase} · Ready` : pod.phase || "—"} />
          <PodStat label={t("project.pod.podip")} value={pod.pod_ip || "—"} mono />
          <PodStat label={t("project.pod.name")} value={pod.pod || "—"} mono />
        </Card>
      )}

      <section className="mb-3 flex items-end justify-between">
        <h2 className="text-[12px] font-medium text-ink-muted">{t("project.recent")}</h2>
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
                  cmd: '<code class="font-mono text-ink">deploy +push</code>',
                }),
              }}
            />
          }
        />
      )}

      {list.length > 0 && (
        <>
          <Table>
            <THead>
              <tr>
                <TH>{t("project.deployments.col.id")}</TH>
                <TH>{t("project.deployments.col.status")}</TH>
                <TH>{t("project.deployments.col.trigger")}</TH>
                <TH>{t("project.deployments.col.image")}</TH>
                <TH>{t("project.deployments.col.started")}</TH>
                <TH>{t("project.deployments.col.deployed")}</TH>
              </tr>
            </THead>
            <TBody>
              {list.map((d) => (
                <TR
                  key={d.id}
                  onClick={() => nav(`/projects/${slug}/deployments/${d.id}`)}
                  className="group cursor-pointer"
                >
                  <TD className="font-mono text-[12px]">
                    <Link
                      to={`/projects/${slug}/deployments/${d.id}`}
                      onClick={(e) => e.stopPropagation()}
                      className="text-ink group-hover:text-ink-strong hover:underline"
                    >
                      {shortID(d.id)}
                    </Link>
                  </TD>
                  <TD>
                    <StatusBadge status={d.status} />
                  </TD>
                  <TD className="text-ink-muted">{d.trigger_type}</TD>
                  <TD className="font-mono text-[11.5px] text-ink-faint max-w-xs truncate">
                    {d.image_tag || t("common.empty")}
                  </TD>
                  <TD className="text-ink-muted tabular-nums">{timeAgo(d.build_started_at)}</TD>
                  <TD className="text-ink-muted tabular-nums">{timeAgo(d.deployed_at)}</TD>
                </TR>
              ))}
            </TBody>
          </Table>
          <Pagination state={pg} total={deployments.data?.total ?? 0} onChange={setPg} />
        </>
      )}

      <div className="mt-10">
        <RuntimePanel slug={slug} />
      </div>

      <div className="mt-8">
        <ProjectNamePanel
          slug={slug}
          initialName={p.name || slug}
          canEdit={canManage}
        />
      </div>

      <div className="mt-8">
        <CollaboratorsPanel slug={slug} ownerEmail={p.owner_email} />
      </div>

      <div className="mt-8">
        <DomainsPanel slug={slug} canEdit={canManage} />
      </div>

      <div className="mt-8">
        <EnvPanel slug={slug} canEdit={canManage} />
      </div>

      {/* Access control panel — withdrawn from the project page. Inbound policy
          (SSO scope + IP allow-list) is no longer self-service: it is owned by
          platform admins through 设置 → 访问预设, and PATCH /access and
          PATCH /visibility both refuse changes server-side. Hiding the panel
          alone would only move the same capability behind a raw API call, which
          is why the backend is gated too.

          Kept commented rather than deleted so re-enabling is a two-line
          revert if the policy changes.

      <div className="mt-8">
        <ProjectAccessPanel
          slug={slug}
          initialPreset={p.access_preset ?? null}
          initialCIDRs={p.allow_cidrs ?? []}
          visibility={p.visibility}
          canEdit={canManage}
          canManagePresets={
            who.data?.kind === "user"
              ? Boolean(who.data.admin)
              : !who.data?.project_id
          }
        />
      </div>
      */}

      <div className="mt-8">
        <ProjectTLSPanel
          slug={slug}
          initialEnabled={p.tls_enabled ?? false}
          canEdit={canManage}
        />
      </div>

      <div className="mt-8">
        <ProjectAPIAccessPanel
          slug={slug}
          initialTokenPrefix={p.api_token_prefix}
          initialTokenCreatedAt={p.api_token_created_at}
          canEdit={canManage}
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
    <Card className="border-status-fail-dot/30">
      <CardHeader
        className="border-status-fail-dot/20"
        title={<span className="text-status-fail-fg">{t("danger.title")}</span>}
        description={t("danger.delete.desc", { name })}
      />
      <CardBody className="grid grid-cols-1 md:grid-cols-[1fr_auto] gap-3 items-end">
        <div>
          <Label className="block text-[11.5px] font-normal text-ink-muted mb-1.5">
            {t("danger.delete.confirm.label", { slug })}
          </Label>
          <Input
            type="text"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder={slug}
            spellCheck={false}
            autoComplete="off"
            className="font-mono"
          />
        </div>
        <Button variant="destructive" className="h-9" onClick={() => del.mutate()} disabled={!armed || del.isPending}>
          {del.isPending ? t("danger.delete.button.busy") : t("danger.delete.button")}
        </Button>
      </CardBody>
      {opError && (
        <div className="px-5 pb-4">
          <Alert variant="danger">{opError}</Alert>
        </div>
      )}
    </Card>
  );
}

function PodStat({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-[12px] font-medium text-ink-muted mb-1">{label}</div>
      <div className={`text-[12.5px] text-ink truncate ${mono ? "font-mono" : ""}`} title={value}>
        {value}
      </div>
    </div>
  );
}
