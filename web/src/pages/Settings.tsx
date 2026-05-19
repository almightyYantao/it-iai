import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";
import { PageContainer, PageHeader } from "../components/Page";
import { ErrorState, LoadingRows } from "../components/States";
import type { CIDRPreset, SystemSettings } from "../lib/types";

const SECRET_PLACEHOLDER = "********";

// Which fields are write-only secrets — we never display the real value,
// only a sentinel indicating "configured" vs "not configured".
const SECRET_FIELDS = new Set(["client_secret", "cookie_secret"]);

type KCKey = keyof SystemSettings["kc"];

// Driver mapping between the form field name and the system_config DB key.
// Drives both the "DB override" badge and the field grouping.
const DB_KEY: Record<KCKey, string> = {
  issuer: "kc.issuer",
  jwks_url: "kc.jwks_url",
  audience: "kc.audience",
  authorization_url: "kc.authorization_url",
  token_url: "kc.token_url",
  client_id: "kc.client_id",
  client_secret: "kc.client_secret",
  redirect_url: "kc.redirect_url",
  auth_host: "auth.host",
  cookie_secret: "auth.cookie_secret",
  cookie_domain: "auth.cookie_domain",
  brand_name: "auth.brand_name",
};

const KC_FIELDS: KCKey[] = [
  "issuer",
  "jwks_url",
  "audience",
  "authorization_url",
  "token_url",
  "client_id",
  "client_secret",
  "redirect_url",
];

const AUTH_FIELDS: KCKey[] = ["brand_name", "auth_host", "cookie_secret", "cookie_domain"];

export function Settings() {
  const { t } = useI18n();
  const qc = useQueryClient();

  const q = useQuery({ queryKey: ["admin", "settings"], queryFn: api.getSystemSettings, retry: false });

  // Local edit buffer. Initialised from the server payload; only fields the
  // user actually touches get sent on save. Secrets that come back as the
  // placeholder are kept as-is so the user can save other fields without
  // re-typing them.
  const [draft, setDraft] = useState<Partial<SystemSettings["kc"]>>({});
  const [toast, setToast] = useState<{ kind: "ok" | "err"; msg: string } | null>(null);

  useEffect(() => {
    if (q.data) setDraft({});
  }, [q.data]);

  const dirty = useMemo(() => Object.keys(draft).length > 0, [draft]);

  const save = useMutation({
    mutationFn: (patch: Partial<SystemSettings["kc"]>) => api.updateSystemSettings({ kc: patch }),
    onSuccess: (res) => {
      setToast({ kind: "ok", msg: t("settings.toast.saved", { count: String(res.changed) }) });
      setDraft({});
      qc.invalidateQueries({ queryKey: ["admin", "settings"] });
    },
    onError: (e: Error) => setToast({ kind: "err", msg: t("settings.toast.failed", { error: e.message }) }),
  });

  if (q.error) {
    return (
      <PageContainer>
        <PageHeader eyebrow={t("settings.eyebrow")} title={t("settings.title")} />
        <ErrorState
          message={
            q.error instanceof ApiError && q.error.status === 403
              ? t("settings.error.noadmin")
              : (q.error as Error).message
          }
          onRetry={() => q.refetch()}
        />
      </PageContainer>
    );
  }

  if (q.isLoading || !q.data) {
    return (
      <PageContainer>
        <PageHeader eyebrow={t("settings.eyebrow")} title={t("settings.title")} description={t("settings.description")} />
        <LoadingRows rows={6} cols={2} />
      </PageContainer>
    );
  }

  const data = q.data;

  function valueOf(field: KCKey): string {
    if (field in draft) return draft[field] ?? "";
    return data.kc[field] ?? "";
  }

  function setField(field: KCKey, v: string) {
    setDraft((d) => {
      const next = { ...d, [field]: v };
      // If the user typed the placeholder verbatim back into a secret, drop
      // the change — saving "********" is a no-op on the server but it's
      // cleaner to not show the field as dirty.
      if (SECRET_FIELDS.has(field) && v === SECRET_PLACEHOLDER) {
        delete (next as Record<string, unknown>)[field];
      }
      return next;
    });
  }

  return (
    <PageContainer>
      <PageHeader
        eyebrow={t("settings.eyebrow")}
        title={t("settings.title")}
        description={t("settings.description")}
        actions={
          <div className="flex items-center gap-2">
            {dirty && (
              <button
                onClick={() => setDraft({})}
                className="px-3 py-1.5 rounded-md text-[12.5px] text-ink-muted hover:text-ink-DEFAULT transition-colors"
              >
                {t("settings.button.reset")}
              </button>
            )}
            <button
              disabled={!dirty || save.isPending}
              onClick={() => save.mutate(draft)}
              className={cn(
                "px-3.5 py-1.5 rounded-md text-[12.5px] font-medium transition-colors",
                dirty && !save.isPending
                  ? "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT"
                  : "bg-canvas-raised text-ink-faint cursor-not-allowed",
              )}
            >
              {save.isPending ? t("settings.button.save.busy") : t("settings.button.save")}
            </button>
          </div>
        }
      />

      {toast && (
        <div
          className={cn(
            "mb-6 rounded-md border px-4 py-2.5 text-[12.5px]",
            toast.kind === "ok"
              ? "border-status-ok-fg/30 bg-status-ok-bg/40 text-status-ok-fg"
              : "border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg",
          )}
          role="status"
        >
          {toast.msg}
        </div>
      )}

      <Section title={t("settings.section.kc.title")} description={t("settings.section.kc.desc")}>
        {KC_FIELDS.map((field) => (
          <Field
            key={field}
            field={field}
            label={t(`settings.field.${field}`)}
            hint={hintKey(field, t)}
            value={valueOf(field)}
            dbMeta={data.meta[DB_KEY[field]]}
            isSecret={SECRET_FIELDS.has(field)}
            onChange={(v) => setField(field, v)}
            t={t}
          />
        ))}
      </Section>

      <Section title={t("settings.section.auth.title")} description={t("settings.section.auth.desc")}>
        {AUTH_FIELDS.map((field) => (
          <Field
            key={field}
            field={field}
            label={t(`settings.field.${field}`)}
            hint={hintKey(field, t)}
            value={valueOf(field)}
            dbMeta={data.meta[DB_KEY[field]]}
            isSecret={SECRET_FIELDS.has(field)}
            onChange={(v) => setField(field, v)}
            t={t}
          />
        ))}
      </Section>

      <CIDRPresetsSection />
    </PageContainer>
  );
}

function hintKey(field: KCKey, t: (k: string) => string): string {
  // Not every field has a hint copy; t() falls back to the key, so guard with
  // a presence check before rendering.
  const key = `settings.field.${field}.hint`;
  const v = t(key);
  return v === key ? "" : v;
}

function Section({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="mb-10">
      <header className="mb-5">
        <h2 className="text-[15px] font-medium text-ink-strong">{title}</h2>
        <p className="mt-1.5 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">{description}</p>
      </header>
      <div className="rounded-lg border border-line-subtle bg-canvas-surface divide-y divide-line-subtle/60">
        {children}
      </div>
    </section>
  );
}

function Field({
  field,
  label,
  hint,
  value,
  dbMeta,
  isSecret,
  onChange,
  t,
}: {
  field: KCKey;
  label: string;
  hint: string;
  value: string;
  dbMeta?: { has_override: boolean; updated_at?: string };
  isSecret: boolean;
  onChange: (v: string) => void;
  t: (k: string, vars?: Record<string, string>) => string;
}) {
  // For secrets the server returns "********" (set) or "" (unset).
  // Show that as a placeholder, never the value; user types a fresh value
  // to overwrite, or leaves it empty + saves "" to clear.
  const showAsSecret = isSecret;
  const inputType = showAsSecret ? "password" : "text";
  const placeholder = showAsSecret
    ? value === SECRET_PLACEHOLDER
      ? t("settings.placeholder.secret.set")
      : t("settings.placeholder.secret.empty")
    : "";
  // For secrets, we don't pre-fill the value (so typing replaces it cleanly).
  const inputValue = showAsSecret && value === SECRET_PLACEHOLDER ? "" : value;

  return (
    <div className="px-5 py-4 grid grid-cols-1 md:grid-cols-[200px_1fr] gap-x-6 gap-y-1.5">
      <div className="pt-1.5">
        <label htmlFor={`f-${field}`} className="block text-[12.5px] font-medium text-ink-DEFAULT">
          {label}
        </label>
        <div className="mt-1.5 flex items-center gap-1.5">
          {dbMeta?.has_override ? (
            <span className="inline-flex items-center rounded-full bg-brand/12 text-brand px-1.5 py-0.5 text-[10px] font-medium tracking-wide uppercase">
              {t("settings.badge.db")}
            </span>
          ) : (
            <span className="inline-flex items-center rounded-full bg-canvas-raised text-ink-faint px-1.5 py-0.5 text-[10px] font-medium tracking-wide uppercase">
              {t("settings.badge.env")}
            </span>
          )}
          {dbMeta?.updated_at && (
            <span className="text-[10.5px] text-ink-faint" title={dbMeta.updated_at}>
              {t("settings.badge.updated", { when: timeAgo(dbMeta.updated_at) })}
            </span>
          )}
        </div>
      </div>
      <div>
        <input
          id={`f-${field}`}
          type={inputType}
          value={inputValue}
          placeholder={placeholder}
          spellCheck={false}
          autoComplete="off"
          onChange={(e) => onChange(e.target.value)}
          className="w-full rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
        />
        {hint && <p className="mt-1.5 text-[11.5px] text-ink-muted leading-relaxed">{hint}</p>}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Access presets — global IP allow-list templates that projects pick from.
// ---------------------------------------------------------------------------

const SLUG_RE = /^[a-z0-9][a-z0-9_-]{2,29}$/;

function CIDRPresetsSection() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["cidr-presets"], queryFn: api.listCIDRPresets });

  // Inline editor state. `editing.name === ""` means "new preset" form.
  const [editing, setEditing] = useState<{
    name: string;
    label: string;
    description: string;
    cidrs: string;
    isNew: boolean;
  } | null>(null);
  const [opError, setOpError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: async () => {
      if (!editing) throw new Error("no draft");
      const cidrs = editing.cidrs
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean);
      return api.upsertCIDRPreset(editing.name, {
        label: editing.label,
        description: editing.description,
        cidrs,
      });
    },
    onSuccess: () => {
      setOpError(null);
      setEditing(null);
      qc.invalidateQueries({ queryKey: ["cidr-presets"] });
    },
    onError: (e) => setOpError(e instanceof ApiError ? e.message : String(e)),
  });

  const del = useMutation({
    mutationFn: (name: string) => api.deleteCIDRPreset(name),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cidr-presets"] }),
    onError: (e) => setOpError(e instanceof ApiError ? e.message : String(e)),
  });

  function startNew() {
    setOpError(null);
    setEditing({ name: "", label: "", description: "", cidrs: "", isNew: true });
  }

  function startEdit(p: CIDRPreset) {
    setOpError(null);
    setEditing({
      name: p.name,
      label: p.label,
      description: p.description,
      cidrs: p.cidrs.join("\n"),
      isNew: false,
    });
  }

  function onDelete(p: CIDRPreset) {
    if (!confirm(t("presets.confirm.delete", { label: p.label }))) return;
    del.mutate(p.name);
  }

  const presets = q.data?.presets ?? [];
  const slugValid = editing?.isNew ? SLUG_RE.test(editing.name) : true;
  const canSave =
    !!editing &&
    editing.label.trim().length > 0 &&
    slugValid &&
    !save.isPending;

  return (
    <section className="mb-10" id="presets">
      <header className="mb-5 flex items-start justify-between gap-6">
        <div>
          <h2 className="text-[15px] font-medium text-ink-strong">{t("presets.section.title")}</h2>
          <p className="mt-1.5 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">
            {t("presets.section.desc")}
          </p>
        </div>
        {!editing && (
          <button
            onClick={startNew}
            className="shrink-0 px-3 py-1.5 rounded-md text-[12.5px] font-medium bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT transition-colors"
          >
            {t("presets.add.button")}
          </button>
        )}
      </header>

      {q.error && (
        <ErrorState
          message={
            q.error instanceof ApiError && q.error.status === 403
              ? t("settings.error.noadmin")
              : (q.error as Error).message
          }
          onRetry={() => q.refetch()}
        />
      )}
      {q.isLoading && <LoadingRows rows={2} cols={2} />}

      {q.data && (
        <div className="rounded-lg border border-line-subtle bg-canvas-surface overflow-hidden">
          <table className="min-w-full text-[13px]">
            <thead className="bg-canvas-base/40">
              <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                <th className="px-5 py-3 font-medium">{t("presets.col.label")}</th>
                <th className="px-5 py-3 font-medium">{t("presets.col.cidrs")}</th>
                <th className="px-5 py-3 font-medium">{t("presets.col.system")}</th>
                <th className="px-5 py-3 font-medium text-right">{t("presets.col.actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-subtle/60">
              {presets.map((p) => (
                <tr key={p.name} className="align-top">
                  <td className="px-5 py-3.5">
                    <div className="text-ink-strong font-medium">{p.label}</div>
                    <div className="text-[11.5px] font-mono text-ink-faint mt-0.5">{p.name}</div>
                    {p.description && (
                      <div className="text-[12px] text-ink-muted mt-1 max-w-md leading-snug">
                        {p.description}
                      </div>
                    )}
                  </td>
                  <td className="px-5 py-3.5 text-[11.5px] text-ink-DEFAULT align-top">
                    {p.cidrs.length === 0 ? (
                      <span className="text-ink-faint italic font-mono">—</span>
                    ) : (
                      <div>
                        <div className="text-[10.5px] uppercase tracking-wide text-ink-faint mb-1 font-sans">
                          {t("presets.cidrs.count", { n: String(p.cidrs.length) })}
                        </div>
                        <div className="max-h-40 overflow-y-auto font-mono leading-relaxed pr-1">
                          {p.cidrs.map((c) => (
                            <div key={c}>{c}</div>
                          ))}
                        </div>
                      </div>
                    )}
                  </td>
                  <td className="px-5 py-3.5">
                    {p.is_system ? (
                      <span
                        className="inline-flex items-center rounded-full bg-canvas-raised text-ink-muted px-1.5 py-0.5 text-[10.5px] font-medium tracking-wide uppercase"
                        title={t("presets.system.hint")}
                      >
                        {t("presets.system.tag")}
                      </span>
                    ) : (
                      <span className="text-ink-faint text-[12px]">—</span>
                    )}
                  </td>
                  <td className="px-5 py-3.5 text-right whitespace-nowrap">
                    <button
                      onClick={() => startEdit(p)}
                      className="text-[12px] text-brand hover:underline mr-3"
                    >
                      {t("presets.button.save")}
                    </button>
                    {!p.is_system && (
                      <button
                        onClick={() => onDelete(p)}
                        disabled={del.isPending}
                        className="text-[12px] text-status-fail-fg hover:underline"
                      >
                        {t("presets.button.delete")}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing && (
        <div className="mt-6 rounded-lg border border-brand/30 bg-brand/5 px-5 py-5 space-y-4">
          <h3 className="text-[14px] font-semibold text-ink-strong">
            {editing.isNew ? t("presets.new.title") : t("presets.edit.title", { label: editing.label || editing.name })}
          </h3>

          {editing.isNew && (
            <PresetField label={t("presets.field.name")} hint={t("presets.field.name.hint")}>
              <input
                value={editing.name}
                onChange={(e) => setEditing({ ...editing, name: e.target.value.toLowerCase() })}
                spellCheck={false}
                autoComplete="off"
                placeholder="internal-vpc"
                className={cn(
                  "w-full rounded-md border bg-canvas-base px-3 py-1.5 text-[13px] font-mono text-ink-strong focus:outline-none focus:ring-2 focus:ring-brand/30",
                  slugValid ? "border-line focus:border-brand" : "border-status-fail-fg focus:border-status-fail-fg",
                )}
              />
            </PresetField>
          )}

          <PresetField label={t("presets.field.label")} hint={t("presets.field.label.hint")}>
            <input
              value={editing.label}
              onChange={(e) => setEditing({ ...editing, label: e.target.value })}
              spellCheck={false}
              className="w-full rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] text-ink-strong focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
            />
          </PresetField>

          <PresetField label={t("presets.field.description")}>
            <input
              value={editing.description}
              onChange={(e) => setEditing({ ...editing, description: e.target.value })}
              className="w-full rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] text-ink-strong focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
            />
          </PresetField>

          <PresetField label={t("presets.field.cidrs")} hint={t("presets.field.cidrs.hint")}>
            <textarea
              value={editing.cidrs}
              onChange={(e) => setEditing({ ...editing, cidrs: e.target.value })}
              spellCheck={false}
              rows={6}
              placeholder="10.0.0.0/8&#10;192.168.1.0/24"
              className="w-full rounded-md border border-line bg-canvas-base px-3 py-2 text-[12.5px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
            />
          </PresetField>

          {opError && (
            <div className="rounded-md border border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg text-[12px] px-3 py-2">
              {opError}
            </div>
          )}

          <div className="flex items-center justify-end gap-2">
            <button
              onClick={() => {
                setEditing(null);
                setOpError(null);
              }}
              className="px-3 py-1.5 rounded-md text-[12.5px] text-ink-muted hover:text-ink-DEFAULT transition-colors"
            >
              {t("presets.button.cancel")}
            </button>
            <button
              onClick={() => save.mutate()}
              disabled={!canSave}
              className={cn(
                "px-3.5 py-1.5 rounded-md text-[12.5px] font-medium transition-colors",
                canSave
                  ? "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT"
                  : "bg-canvas-raised text-ink-faint cursor-not-allowed",
              )}
            >
              {save.isPending ? t("presets.button.save.busy") : t("presets.button.save")}
            </button>
          </div>
        </div>
      )}
    </section>
  );
}

function PresetField({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-[200px_1fr] gap-x-6 gap-y-1.5">
      <label className="pt-1.5 text-[12.5px] font-medium text-ink-DEFAULT">{label}</label>
      <div>
        {children}
        {hint && <p className="mt-1.5 text-[11.5px] text-ink-muted leading-relaxed">{hint}</p>}
      </div>
    </div>
  );
}
