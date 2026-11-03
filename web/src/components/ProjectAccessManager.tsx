import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import type { CIDRPreset, Project, ProjectPathRule } from "../lib/types";

// Admin-only access manager: project list on the left, the selected project's
// preset and path rules on the right. External sources live on a single rule,
// never on the whole project, and force token mode.

type Mode = "no_auth" | "token";
type Notify = (ok: boolean, text: string) => void;

const errText = (e: unknown) => (e instanceof ApiError ? e.message : String(e));
const splitIPs = (v: string) =>
  v.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);

const fieldCls =
  "w-full rounded-lg border border-line bg-white px-3 py-2 text-[13px] text-ink-strong placeholder:text-ink-faint " +
  "outline-none transition focus:border-brand focus:ring-4 focus:ring-brand/20";

export function ProjectAccessManager() {
  const { t } = useI18n();
  const projects = useQuery({ queryKey: ["admin-projects-all"], queryFn: () => api.listAllProjects(200, 0) });
  const presets = useQuery({ queryKey: ["cidr-presets"], queryFn: api.listCIDRPresets });
  const [q, setQ] = useState("");
  const [slug, setSlug] = useState<string | null>(null);
  const [toast, setToast] = useState<{ ok: boolean; text: string } | null>(null);

  const notify: Notify = (ok, text) => {
    setToast({ ok, text });
    window.setTimeout(() => setToast(null), 2600);
  };

  const all = projects.data?.projects ?? [];
  const list = useMemo(() => {
    const k = q.trim().toLowerCase();
    return k ? all.filter((p) => p.name.toLowerCase().includes(k) || p.slug.includes(k)) : all;
  }, [all, q]);
  const presetList = (presets.data?.presets ?? []).filter((p) => p.name !== "public");
  const labelOf = (name?: string | null) =>
    presetList.find((p) => p.name === (name || "internal"))?.label ?? (name || "internal");
  const selected = all.find((p) => p.slug === slug) ?? null;

  return (
    <section className="mb-10" id="project-access">
      <div className="mb-4">
        <h2 className="text-[15px] font-semibold text-ink-strong">{t("pa.title")}</h2>
        <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed">{t("pa.desc")}</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[320px_1fr] overflow-hidden rounded-2xl border border-line-subtle bg-white shadow-soft">
        {/* Project list */}
        <aside className="border-b lg:border-b-0 lg:border-r border-line-subtle bg-canvas-surface/60">
          <div className="p-3">
            <div className="relative">
              <svg className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-ink-faint" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8">
                <circle cx="9" cy="9" r="6" /><path d="m14 14 4 4" strokeLinecap="round" />
              </svg>
              <input
                className={cn(fieldCls, "pl-9 rounded-full")}
                placeholder={t("pa.search", { n: String(all.length) })}
                value={q}
                onChange={(e) => setQ(e.target.value)}
              />
            </div>
          </div>
          <ul className="max-h-[560px] overflow-y-auto px-2 pb-3 space-y-0.5">
            {list.map((p) => {
              const active = p.slug === slug;
              return (
                <li key={p.slug}>
                  <button
                    onClick={() => setSlug(p.slug)}
                    className={cn(
                      "group w-full rounded-xl px-3 py-2.5 text-left transition",
                      active ? "bg-brand text-white shadow-raised" : "hover:bg-white hover:shadow-soft",
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className={cn("truncate text-[13px] font-medium", active ? "text-white" : "text-ink-strong")}>{p.name}</span>
                      <span className={cn(
                        "shrink-0 rounded-full px-2 py-0.5 text-[10.5px] font-medium",
                        active ? "bg-white/20 text-white" : "bg-canvas-raised text-ink-muted",
                      )}>
                        {labelOf(p.access_preset)}
                      </span>
                    </div>
                    <div className={cn("mt-0.5 truncate font-mono text-[11px]", active ? "text-white/75" : "text-ink-faint")}>{p.slug}</div>
                  </button>
                </li>
              );
            })}
            {list.length === 0 && <li className="px-3 py-8 text-center text-[12px] text-ink-faint">{t("pa.empty")}</li>}
          </ul>
        </aside>

        {/* Detail */}
        <div className="min-h-[420px]">
          {selected ? (
            <ProjectPane key={selected.slug} project={selected} presets={presetList} notify={notify} />
          ) : (
            <div className="flex h-full min-h-[420px] flex-col items-center justify-center gap-3 text-ink-faint">
              <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-canvas-raised">
                <svg className="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6">
                  <path d="M12 3 4 6v6c0 4.5 3.4 8 8 9 4.6-1 8-4.5 8-9V6l-8-3Z" strokeLinejoin="round" />
                </svg>
              </div>
              <p className="text-[13px]">{t("pa.pick")}</p>
            </div>
          )}
        </div>
      </div>

      {toast && (
        <div className={cn(
          "fixed bottom-6 left-1/2 z-50 -translate-x-1/2 rounded-full px-4 py-2 text-[12.5px] font-medium text-white shadow-raised",
          toast.ok ? "bg-ink-strong" : "bg-red-600",
        )}>
          {toast.text}
        </div>
      )}
    </section>
  );
}

function ProjectPane({ project, presets, notify }: { project: Project; presets: CIDRPreset[]; notify: Notify }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const rules = useQuery({ queryKey: ["path-rules", project.slug], queryFn: () => api.listProjectPathRules(project.slug) });
  const [editing, setEditing] = useState<string | "new" | null>(null);
  const current = project.access_preset || "internal";

  const setPreset = useMutation({
    mutationFn: (preset: string) => api.setProjectAccess(project.slug, { preset }),
    onSuccess: () => {
      notify(true, t("pa.saved"));
      qc.invalidateQueries({ queryKey: ["admin-projects-all"] });
    },
    onError: (e) => notify(false, errText(e)),
  });

  const list = rules.data?.rules ?? [];
  const rootOpen = list.some((r) => r.path_prefix === "/" && r.mode === "no_auth");

  return (
    <div className="p-6 space-y-7">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate text-[17px] font-semibold text-ink-strong">{project.name}</h3>
          <a href={project.url} target="_blank" rel="noreferrer" className="font-mono text-[12px] text-brand hover:underline">
            {project.url.replace(/^https?:\/\//, "")} ↗
          </a>
        </div>
      </header>

      {/* Preset */}
      <div>
        <div className="mb-2 flex items-baseline justify-between">
          <h4 className="text-[13px] font-semibold text-ink-strong">{t("pa.preset")}</h4>
          <span className="text-[11.5px] text-ink-faint">{t("pa.preset.hint")}</span>
        </div>
        <div className="inline-flex flex-wrap gap-1 rounded-xl bg-canvas-raised p-1">
          {presets.map((p) => (
            <button
              key={p.name}
              disabled={setPreset.isPending}
              onClick={() => p.name !== current && setPreset.mutate(p.name)}
              className={cn(
                "rounded-lg px-3.5 py-1.5 text-[12.5px] font-medium transition",
                p.name === current ? "bg-white text-ink-strong shadow-soft" : "text-ink-muted hover:text-ink-strong",
              )}
            >
              {p.label}
              <span className="ml-1.5 text-[11px] text-ink-faint">{p.cidrs.length}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Rules */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <div>
            <h4 className="text-[13px] font-semibold text-ink-strong">{t("pa.rules")}</h4>
            <p className="text-[11.5px] text-ink-faint">{t("pa.rules.hint")}</p>
          </div>
          {editing !== "new" && (
            <button
              onClick={() => setEditing("new")}
              className="inline-flex items-center gap-1.5 rounded-full bg-brand px-3.5 py-1.5 text-[12.5px] font-medium text-white shadow-soft transition hover:bg-brand-hover"
            >
              <span className="text-[15px] leading-none">+</span>{t("pa.rule.add")}
            </button>
          )}
        </div>

        {rootOpen && (
          <div className="mb-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-2.5 text-[12px] text-amber-800">
            ⚠ {t("pa.warn.root")}
          </div>
        )}

        <div className="space-y-2">
          {editing === "new" && (
            <RuleForm slug={project.slug} notify={notify} onClose={() => setEditing(null)} />
          )}
          {list.map((r) =>
            editing === r.id ? (
              <RuleForm key={r.id} slug={project.slug} rule={r} notify={notify} onClose={() => setEditing(null)} />
            ) : (
              <RuleRow key={r.id} slug={project.slug} rule={r} notify={notify} onEdit={() => setEditing(r.id)} />
            ),
          )}
          {list.length === 0 && editing !== "new" && !rules.isLoading && (
            <div className="rounded-xl border border-dashed border-line px-4 py-8 text-center text-[12.5px] text-ink-faint">
              {t("pa.rules.empty")}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function ModeBadge({ mode }: { mode: Mode }) {
  const { t } = useI18n();
  return (
    <span className={cn(
      "rounded-full px-2 py-0.5 text-[11px] font-medium",
      mode === "token" ? "bg-status-flight-bg text-status-flight-fg" : "bg-status-warn-bg text-status-warn-fg",
    )}>
      {t(`pa.mode.${mode}`)}
    </span>
  );
}

function RuleRow({ slug, rule, notify, onEdit }: { slug: string; rule: ProjectPathRule; notify: Notify; onEdit: () => void }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const ext = rule.allow_cidrs ?? [];
  const del = useMutation({
    mutationFn: () => api.deleteProjectPathRule(slug, rule.id),
    onSuccess: () => {
      notify(true, t("pa.deleted"));
      qc.invalidateQueries({ queryKey: ["path-rules", slug] });
    },
    onError: (e) => notify(false, errText(e)),
  });

  return (
    <div className="group flex items-center gap-3 rounded-xl border border-line-subtle bg-white px-4 py-3 transition hover:border-line hover:shadow-soft">
      <code className="min-w-0 flex-1 truncate font-mono text-[13px] text-ink-strong">{rule.path_prefix}</code>
      <ModeBadge mode={rule.mode} />
      {ext.length > 0 ? (
        <span title={ext.join("\n")} className="rounded-full bg-emerald-50 px-2 py-0.5 text-[11px] font-medium text-emerald-700">
          🌐 {t("pa.ext.count", { n: String(ext.length) })}
        </span>
      ) : (
        <span className="rounded-full bg-canvas-raised px-2 py-0.5 text-[11px] text-ink-muted">{t("pa.internal_only")}</span>
      )}
      <div className="flex gap-1 opacity-60 transition group-hover:opacity-100">
        <button onClick={onEdit} className="rounded-lg px-2.5 py-1 text-[12px] text-ink-muted hover:bg-canvas-raised hover:text-ink-strong">
          {t("pa.edit")}
        </button>
        <button
          disabled={del.isPending}
          onClick={() => confirm(t("pa.delete.confirm", { path: rule.path_prefix })) && del.mutate()}
          className="rounded-lg px-2.5 py-1 text-[12px] text-ink-muted hover:bg-red-50 hover:text-red-600"
        >
          {t("pa.delete")}
        </button>
      </div>
    </div>
  );
}

function RuleForm({ slug, rule, notify, onClose }: { slug: string; rule?: ProjectPathRule; notify: Notify; onClose: () => void }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [prefix, setPrefix] = useState(rule?.path_prefix ?? "");
  const [mode, setMode] = useState<Mode>(rule?.mode ?? "token");
  const [extOn, setExtOn] = useState((rule?.allow_cidrs ?? []).length > 0);
  const [ips, setIps] = useState((rule?.allow_cidrs ?? []).join("\n"));
  const ext = extOn ? splitIPs(ips) : [];
  const valid = prefix.startsWith("/") && (!extOn || ext.length > 0);

  const save = useMutation({
    mutationFn: () => {
      const body = { path_prefix: prefix.trim(), mode, allow_cidrs: ext };
      return rule ? api.updateProjectPathRule(slug, rule.id, body) : api.createProjectPathRule(slug, body);
    },
    onSuccess: () => {
      notify(true, t("pa.saved"));
      qc.invalidateQueries({ queryKey: ["path-rules", slug] });
      onClose();
    },
    onError: (e) => notify(false, errText(e)),
  });

  const toggleExt = () => {
    const next = !extOn;
    setExtOn(next);
    if (next) setMode("token"); // external sources always need the token
  };

  return (
    <div className="rounded-xl border border-brand/40 bg-white p-4 shadow-raised ring-4 ring-brand/10 space-y-4">
      <div>
        <label className="mb-1.5 block text-[12px] font-medium text-ink-DEFAULT">{t("pa.rule.prefix")}</label>
        <input autoFocus className={cn(fieldCls, "font-mono")} placeholder="/webhook" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
        {prefix === "/" && <p className="mt-1 text-[11.5px] text-amber-700">{t("pa.warn.root")}</p>}
      </div>

      <div>
        <label className="mb-1.5 block text-[12px] font-medium text-ink-DEFAULT">{t("pa.rule.mode")}</label>
        <div className="grid grid-cols-2 gap-2">
          {(["token", "no_auth"] as Mode[]).map((m) => {
            const disabled = extOn && m === "no_auth";
            return (
              <button
                key={m}
                disabled={disabled}
                onClick={() => setMode(m)}
                className={cn(
                  "rounded-xl border px-3 py-2.5 text-left transition",
                  mode === m ? "border-brand bg-brand/5" : "border-line-subtle hover:border-line",
                  disabled && "cursor-not-allowed opacity-40",
                )}
              >
                <div className="text-[12.5px] font-medium text-ink-strong">{t(`pa.mode.${m}`)}</div>
                <div className="mt-0.5 text-[11px] text-ink-muted">{t(`pa.mode.${m}.hint`)}</div>
              </button>
            );
          })}
        </div>
      </div>

      <div className="rounded-xl bg-canvas-surface p-3">
        <button onClick={toggleExt} className="flex w-full items-center justify-between text-left">
          <div>
            <div className="text-[12.5px] font-medium text-ink-strong">{t("pa.ext.toggle")}</div>
            <div className="text-[11px] text-ink-muted">{t("pa.ext.hint")}</div>
          </div>
          <span className={cn("relative h-5 w-9 shrink-0 rounded-full transition", extOn ? "bg-brand" : "bg-line")}>
            <span className={cn("absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-all", extOn ? "left-[18px]" : "left-0.5")} />
          </span>
        </button>
        {extOn && (
          <textarea
            className={cn(fieldCls, "mt-3 h-28 font-mono text-[12px]")}
            placeholder={t("pa.ext.placeholder")}
            value={ips}
            onChange={(e) => setIps(e.target.value)}
          />
        )}
        {extOn && ext.length > 0 && <p className="mt-1 text-[11px] text-ink-faint">{t("pa.ext.count", { n: String(ext.length) })}</p>}
      </div>

      <div className="flex justify-end gap-2">
        <button onClick={onClose} className="rounded-full px-4 py-1.5 text-[12.5px] text-ink-muted hover:bg-canvas-raised">
          {t("pa.cancel")}
        </button>
        <button
          disabled={!valid || save.isPending}
          onClick={() => save.mutate()}
          className={cn(
            "rounded-full px-4 py-1.5 text-[12.5px] font-medium transition",
            valid ? "bg-brand text-white hover:bg-brand-hover" : "bg-canvas-raised text-ink-faint cursor-not-allowed",
          )}
        >
          {save.isPending ? "…" : t("pa.save")}
        </button>
      </div>
    </div>
  );
}
