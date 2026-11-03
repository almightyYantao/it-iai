import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";
import { PageContainer, PageHeader } from "../components/Page";
import { ErrorState, LoadingRows } from "../components/States";
import type { SystemSettings } from "../lib/types";

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
                className="px-3 py-1.5 rounded-md text-[12.5px] text-ink-muted hover:text-ink transition-colors"
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
                  ? "bg-ink-strong text-canvas-base hover:bg-ink h-8 inline-flex items-center justify-center gap-1.5 font-medium"
                  : "bg-canvas-raised text-ink-muted border border-line cursor-not-allowed",
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
      <div className="rounded-lg border border-line-subtle bg-canvas-base divide-y divide-line-subtle">
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
        <label htmlFor={`f-${field}`} className="block text-[12.5px] font-medium text-ink">
          {label}
        </label>
        <div className="mt-1.5 flex items-center gap-1.5">
          {dbMeta?.has_override ? (
            <span className="inline-flex items-center rounded-full bg-canvas-raised text-ink-strong px-1.5 py-0.5 text-[10px] font-medium">
              {t("settings.badge.db")}
            </span>
          ) : (
            <span className="inline-flex items-center rounded-full bg-canvas-raised text-ink-faint px-1.5 py-0.5 text-[10px] font-medium">
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
          className="w-full rounded-md border border-line bg-canvas-base px-3 py-2 text-[13px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-[3px] focus:ring-ink-strong/10 focus:border-line-strong"
        />
        {hint && <p className="mt-1.5 text-[11.5px] text-ink-muted leading-relaxed">{hint}</p>}
      </div>
    </div>
  );
}
