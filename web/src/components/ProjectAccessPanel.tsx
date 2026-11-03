import { Alert, Badge, Button, Card, CardBody, CardFooter, CardHeader, Label, Select, Textarea } from "./ui";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
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

  // Visibility is a single-dropdown setting with a higher blast radius than a
  // CIDR list, so save immediately on change and show inline feedback.
  //
  // "public" was withdrawn — the platform is internal-access-only. It can still
  // arrive from the server on pre-existing rows, which is why the type below
  // stays wide enough to render one (see the legacy <option>), but it can no
  // longer be selected.
  const saveVisibility = useMutation({
    mutationFn: (next: "org" | "restricted") =>
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
    <Card>
      <CardHeader
        title={t("access.title")}
        description={t("access.desc")}
        actions={
          <>
            <Badge
              variant={requiresLogin ? "solid" : "default"}
              title={requiresLogin ? t("access.auth.required.hint") : t("access.auth.open.hint")}
            >
              {requiresLogin ? t("access.auth.required") : t("access.auth.open")}
            </Badge>
            <Badge
              variant={open ? "default" : "success"}
              title={open ? t("access.state.open.hint") : t("access.state.restricted.hint")}
            >
              {open ? t("access.state.open") : t("access.state.restricted")}
            </Badge>
          </>
        }
      />

      <CardBody className="space-y-4">
        {/* Visibility selector — controls Longbridge SSO gating. Saved immediately
            on change because (a) it's a single dropdown so a Save button is
            redundant noise and (b) the change is high-impact enough that
            users should see the result without an extra click. */}
        <div className="grid grid-cols-1 md:grid-cols-[160px_1fr] gap-x-6 gap-y-1.5">
          <Label className="pt-1.5 text-[12.5px]">{t("access.visibility.label")}</Label>
          <div>
            <Select value={visibility} disabled={!canEdit || saveVisibility.isPending}
              onChange={(v) => v !== "public" && saveVisibility.mutate(v as "org" | "restricted")}
              options={[
                { value: "org", label: t("access.visibility.org") },
                { value: "restricted", label: t("access.visibility.restricted") },
                ...(visibility === "public" ? [{ value: "public", label: t("access.visibility.public.legacy") }] : []),
              ]} />
            <p className="mt-1.5 text-[11.5px] text-ink-faint leading-relaxed">
              {visibility === "public"
                ? t("access.visibility.public.legacy.hint")
                : visibility === "restricted"
                  ? t("access.visibility.restricted.hint")
                  : t("access.visibility.org.hint")}
            </p>
          </div>
        </div>

        {/* CIDR mode selector */}
        <div className="grid grid-cols-1 md:grid-cols-[160px_1fr] gap-x-6 gap-y-1.5">
          <Label className="pt-1.5 text-[12.5px]">{t("access.mode.label")}</Label>
          <div>
            <Select value={mode} disabled={!canEdit || save.isPending} onChange={setMode}
              options={[
                ...presets.map((p) => ({ value: p.name, label: p.label })),
                ...(!isCustom && initialPreset && !presetByName.has(initialPreset) ? [{ value: initialPreset, label: initialPreset }] : []),
                { value: CUSTOM, label: t("access.mode.custom") },
              ]} />
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

        {opError && <Alert variant="danger">{opError}</Alert>}
      </CardBody>

      {canEdit && (
        <CardFooter className="justify-between gap-3">
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
              <Button variant="ghost" onClick={reset}>
                {t("access.button.reset")}
              </Button>
            )}
            <Button onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
              {save.isPending ? t("access.button.save.busy") : t("access.button.save")}
            </Button>
          </div>
        </CardFooter>
      )}
    </Card>
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
      <div className="pt-1.5 text-[12.5px] font-medium text-ink">
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
            <div className="px-3 pt-2 pb-1 text-[12px] text-ink-faint">
              {t("presets.cidrs.count", { n: String(preset.cidrs.length) })}
            </div>
            <div className="px-3 pb-2 max-h-48 overflow-y-auto font-mono text-[12.5px] text-ink space-y-0.5">
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
      <div className="pt-1.5 text-[12.5px] font-medium text-ink">
        {t("access.preset.list.label")}
      </div>
      <div>
        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          disabled={disabled}
          spellCheck={false}
          rows={6}
          placeholder={t("access.custom.placeholder")}
          className="font-mono"
        />
        <p className="mt-2 text-[11.5px] text-ink-muted leading-relaxed">
          {t("access.mode.custom.hint")}
        </p>
      </div>
    </div>
  );
}
