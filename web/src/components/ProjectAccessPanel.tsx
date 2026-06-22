import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import type { CIDRPreset } from "../lib/types";

// Sentinel for "custom mode" in the dropdown. Empty string would collide
// with "preset cleared = open" in the API model, so we use a value that
// can't ever be a real preset name (preset names are slug-shape: [a-z0-9_-]).
const CUSTOM = "__custom__";

// ProjectAccessPanel — pick a global preset OR maintain a per-project list.
//
// UX: dropdown drives the mode.
//   - selected = a preset name  → read-only preview of that preset's CIDRs
//   - selected = "Custom"        → editable textarea, one CIDR per line
//
// We keep the draft state local so users can fiddle freely; only push on Save.
// Empty list (in both modes) is explicitly shown as "open to all" to keep
// people from clicking save and accidentally publishing.
export function ProjectAccessPanel({
  slug,
  initialPreset,
  initialCIDRs,
  visibility,
  canEdit,
  canManagePresets,
}: {
  slug: string;
  initialPreset: string | null;
  initialCIDRs: string[];
  visibility: string;
  canEdit: boolean;
  canManagePresets: boolean; // gate the "Manage presets →" deep-link
}) {
  const { t } = useI18n();
  const qc = useQueryClient();

  // Presets list — needed so the dropdown can show "Internal access" rather
  // than the raw slug. Admins fetch presets directly, but the panel is shown
  // to project owners (non-admins) too, so we tolerate a 403 and just hide
  // unknown presets behind their slug.
  const presetsQ = useQuery({
    queryKey: ["cidr-presets"],
    queryFn: api.listCIDRPresets,
    retry: false,
  });
  const presets: CIDRPreset[] = presetsQ.data?.presets ?? [];
  const presetByName = useMemo(() => {
    const m = new Map<string, CIDRPreset>();
    for (const p of presets) m.set(p.name, p);
    return m;
  }, [presets]);

  const [mode, setMode] = useState<string>(initialPreset ?? CUSTOM);
  const [customDraft, setCustomDraft] = useState<string>(initialCIDRs.join("\n"));
  const [opError, setOpError] = useState<string | null>(null);
  const [savedFlash, setSavedFlash] = useState(false);

  // Re-sync when the server view changes (other tab saved, or project reload).
  useEffect(() => {
    setMode(initialPreset ?? CUSTOM);
  }, [initialPreset]);
  useEffect(() => {
    setCustomDraft(initialCIDRs.join("\n"));
  }, [initialCIDRs.join("|")]);

  const customCIDRs = customDraft
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean);

  const isCustom = mode === CUSTOM;
  const dirty =
    (initialPreset ?? CUSTOM) !== mode ||
    (isCustom && customCIDRs.join("|") !== initialCIDRs.join("|"));

  // Effective CIDR list — what's actually enforced.
  // Drives the "open/restricted" badge so users see the consequence of their
  // choice before saving.
  const effectiveCIDRs = isCustom ? customCIDRs : (presetByName.get(mode)?.cidrs ?? []);
  const open = effectiveCIDRs.length === 0;
  const requiresLogin = visibility !== "public";

  const save = useMutation({
    mutationFn: () => {
      if (isCustom) {
        return api.setProjectAccess(slug, { preset: null, allowCIDRs: customCIDRs });
      }
      return api.setProjectAccess(slug, { preset: mode });
    },
    onSuccess: () => {
      setOpError(null);
      setSavedFlash(true);
      setTimeout(() => setSavedFlash(false), 1800);
      qc.invalidateQueries({ queryKey: ["project", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  // Visibility is a single-dropdown setting with a much higher blast radius
  // than a CIDR list (switching to "public" removes SSO entirely). Save
  // immediately on change so the change is obvious, but show inline feedback.
  const saveVisibility = useMutation({
    mutationFn: (next: "org" | "restricted" | "public") =>
      api.setProjectVisibility(slug, next),
    onSuccess: () => {
      setOpError(null);
      setSavedFlash(true);
      setTimeout(() => setSavedFlash(false), 1800);
      qc.invalidateQueries({ queryKey: ["project", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  function reset() {
    setMode(initialPreset ?? CUSTOM);
    setCustomDraft(initialCIDRs.join("\n"));
  }

  return (
    <section className="rounded-lg border border-line-subtle bg-canvas-surface">
      <header className="flex items-start justify-between gap-4 px-5 py-4 border-b border-line-subtle/60">
        <div>
          <h3 className="text-[14px] font-semibold text-ink-strong">{t("access.title")}</h3>
          <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">{t("access.desc")}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span
            className={cn(
              "inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium tracking-wide uppercase",
              requiresLogin ? "bg-brand/12 text-brand" : "bg-canvas-raised text-ink-muted",
            )}
            title={requiresLogin ? t("access.auth.required.hint") : t("access.auth.open.hint")}
          >
            {requiresLogin ? t("access.auth.required") : t("access.auth.open")}
          </span>
          <span
            className={cn(
              "inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium tracking-wide uppercase",
              open ? "bg-canvas-raised text-ink-muted" : "bg-status-ok-bg/60 text-status-ok-fg",
            )}
            title={open ? t("access.state.open.hint") : t("access.state.restricted.hint")}
          >
            {open ? t("access.state.open") : t("access.state.restricted")}
          </span>
        </div>
      </header>

      <div className="px-5 py-4 space-y-4">
        {/* Visibility selector — controls Longbridge SSO gating. Saved immediately
            on change because (a) it's a single dropdown so a Save button is
            redundant noise and (b) the change is high-impact enough that
            users should see the result without an extra click. */}
        <div className="grid grid-cols-1 md:grid-cols-[160px_1fr] gap-x-6 gap-y-1.5">
          <label className="pt-1.5 text-[12.5px] font-medium text-ink-DEFAULT">
            {t("access.visibility.label")}
          </label>
          <div>
            <select
              value={visibility}
              onChange={(e) => saveVisibility.mutate(e.target.value as "org" | "restricted" | "public")}
              disabled={!canEdit || saveVisibility.isPending}
              className="w-full rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] text-ink-strong focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand disabled:opacity-60"
            >
              <option value="org">{t("access.visibility.org")}</option>
              <option value="restricted">{t("access.visibility.restricted")}</option>
              <option value="public">{t("access.visibility.public")}</option>
            </select>
            <p className="mt-1.5 text-[11.5px] text-ink-faint leading-relaxed">
              {visibility === "public"
                ? t("access.visibility.public.hint")
                : visibility === "restricted"
                  ? t("access.visibility.restricted.hint")
                  : t("access.visibility.org.hint")}
            </p>
          </div>
        </div>

        {/* CIDR mode selector */}
        <div className="grid grid-cols-1 md:grid-cols-[160px_1fr] gap-x-6 gap-y-1.5">
          <label className="pt-1.5 text-[12.5px] font-medium text-ink-DEFAULT">
            {t("access.mode.label")}
          </label>
          <div>
            <select
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              disabled={!canEdit || save.isPending}
              className="w-full rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] text-ink-strong focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand disabled:opacity-60"
            >
              {presets.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.label}
                </option>
              ))}
              {/* If the project points at a preset that the current viewer
                  can't list (403 on /admin/cidr-presets), still show it. */}
              {!isCustom && initialPreset && !presetByName.has(initialPreset) && (
                <option value={initialPreset}>{initialPreset}</option>
              )}
              <option value={CUSTOM}>{t("access.mode.custom")}</option>
            </select>
          </div>
        </div>

        {/* Mode body: either preset preview (read-only) or custom textarea. */}
        {isCustom ? (
          <CustomModeBody
            draft={customDraft}
            setDraft={setCustomDraft}
            disabled={!canEdit || save.isPending}
            t={t}
          />
        ) : (
          <PresetModeBody
            preset={presetByName.get(mode)}
            canManagePresets={canManagePresets}
            t={t}
          />
        )}

        {opError && (
          <div className="rounded-md border border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg text-[12px] px-3 py-2">
            {opError}
          </div>
        )}

        {canEdit && (
          <div className="flex items-center justify-between gap-3 pt-1">
            <div className="text-[11.5px] text-ink-faint">
              {open
                ? isCustom
                  ? t("access.count.none")
                  : t("access.count.none.preset")
                : t("access.count.n", { n: String(effectiveCIDRs.length) })}
              {savedFlash && (
                <span className="ml-3 text-status-ok-fg">{t("access.toast.saved")}</span>
              )}
            </div>
            <div className="flex items-center gap-2">
              {dirty && (
                <button
                  onClick={reset}
                  className="px-3 py-1.5 rounded-md text-[12.5px] text-ink-muted hover:text-ink-DEFAULT transition-colors"
                >
                  {t("access.button.reset")}
                </button>
              )}
              <button
                onClick={() => save.mutate()}
                disabled={!dirty || save.isPending}
                className={cn(
                  "px-3.5 py-1.5 rounded-md text-[12.5px] font-medium transition-colors",
                  dirty && !save.isPending
                    ? "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT"
                    : "bg-canvas-raised text-ink-faint cursor-not-allowed",
                )}
              >
                {save.isPending ? t("access.button.save.busy") : t("access.button.save")}
              </button>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}

function PresetModeBody({
  preset,
  canManagePresets,
  t,
}: {
  preset: CIDRPreset | undefined;
  canManagePresets: boolean;
  t: (k: string, vars?: Record<string, string>) => string;
}) {
  // Preset might be undefined if the viewer can't list presets (non-admin).
  // Show whatever we have without crashing — the dropdown still shows the
  // raw slug so the user knows which preset they're on.
  if (!preset) {
    return null;
  }
  return (
    <div className="grid grid-cols-1 md:grid-cols-[160px_1fr] gap-x-6 gap-y-1.5">
      <div className="pt-1.5 text-[12.5px] font-medium text-ink-DEFAULT">
        {t("access.preset.list.label")}
      </div>
      <div>
        {preset.description && (
          <p className="mb-2 text-[12px] text-ink-muted leading-relaxed">{preset.description}</p>
        )}
        {preset.cidrs.length === 0 ? (
          <div className="text-[12px] text-ink-faint italic">{t("access.preset.empty.cidrs")}</div>
        ) : (
          <div className="rounded-md border border-line bg-canvas-base">
            <div className="px-3 pt-2 pb-1 text-[10.5px] uppercase tracking-wide text-ink-faint">
              {t("presets.cidrs.count", { n: String(preset.cidrs.length) })}
            </div>
            <div className="px-3 pb-2 max-h-48 overflow-y-auto font-mono text-[12.5px] text-ink-DEFAULT space-y-0.5">
              {preset.cidrs.map((c) => (
                <div key={c}>{c}</div>
              ))}
            </div>
          </div>
        )}
        {canManagePresets && (
          <Link
            to="/settings#presets"
            className="mt-2 inline-block text-[11.5px] text-brand hover:underline"
          >
            {t("access.preset.manage")}
          </Link>
        )}
      </div>
    </div>
  );
}

function CustomModeBody({
  draft,
  setDraft,
  disabled,
  t,
}: {
  draft: string;
  setDraft: (s: string) => void;
  disabled: boolean;
  t: (k: string) => string;
}) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-[160px_1fr] gap-x-6 gap-y-1.5">
      <div className="pt-1.5 text-[12.5px] font-medium text-ink-DEFAULT">
        {t("access.preset.list.label")}
      </div>
      <div>
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          disabled={disabled}
          spellCheck={false}
          rows={6}
          placeholder={t("access.custom.placeholder")}
          className={cn(
            "w-full rounded-md border border-line bg-canvas-base px-3 py-2 text-[12.5px] font-mono text-ink-strong placeholder:text-ink-faint",
            "focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand",
            disabled && "opacity-60 cursor-not-allowed",
          )}
        />
        <p className="mt-2 text-[11.5px] text-ink-muted leading-relaxed">
          {t("access.mode.custom.hint")}
        </p>
      </div>
    </div>
  );
}
