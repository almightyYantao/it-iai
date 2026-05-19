import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";

// Per-project HTTPS opt-in. Off by default; flipping it on annotates the
// Ingress with the configured cert-manager ClusterIssuer (see
// CP_TLS_CLUSTER_ISSUER on the control-plane) and adds a tls: section,
// causing cert-manager to issue per-host Let's Encrypt certs via HTTP-01.
export function ProjectTLSPanel({
  slug,
  initialEnabled,
  canEdit,
}: {
  slug: string;
  initialEnabled: boolean;
  canEdit: boolean;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();

  const [enabled, setEnabled] = useState(initialEnabled);
  const [opError, setOpError] = useState<string | null>(null);
  const [savedFlash, setSavedFlash] = useState(false);

  // Re-sync if the server view changes underneath us (other tab, project reload).
  useEffect(() => {
    setEnabled(initialEnabled);
  }, [initialEnabled]);

  const dirty = enabled !== initialEnabled;

  const save = useMutation({
    mutationFn: () => api.setProjectTLS(slug, enabled),
    onSuccess: () => {
      setOpError(null);
      setSavedFlash(true);
      setTimeout(() => setSavedFlash(false), 2400);
      qc.invalidateQueries({ queryKey: ["project", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  return (
    <section className="rounded-lg border border-line-subtle bg-canvas-surface">
      <header className="flex items-start justify-between gap-4 px-5 py-4 border-b border-line-subtle/60">
        <div>
          <h3 className="text-[14px] font-semibold text-ink-strong">{t("tls.title")}</h3>
          <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">
            {t("tls.desc")}
          </p>
        </div>
        <span
          className={cn(
            "inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium tracking-wide uppercase shrink-0",
            initialEnabled
              ? "bg-status-ok-bg/60 text-status-ok-fg"
              : "bg-canvas-raised text-ink-muted",
          )}
        >
          {initialEnabled ? t("tls.state.on") : t("tls.state.off")}
        </span>
      </header>

      <div className="px-5 py-4 space-y-3">
        <label
          className={cn(
            "flex items-start gap-3 text-[13px] text-ink-strong select-none",
            !canEdit && "opacity-60 cursor-not-allowed",
          )}
        >
          <input
            type="checkbox"
            checked={enabled}
            disabled={!canEdit || save.isPending}
            onChange={(e) => setEnabled(e.target.checked)}
            className="mt-0.5 h-4 w-4 rounded border-line accent-brand"
          />
          <span>{t("tls.label.enabled")}</span>
        </label>

        <p className="text-[11.5px] text-ink-muted leading-relaxed">
          {enabled ? t("tls.hint.on") : t("tls.hint.off")}
        </p>

        {opError && (
          <div className="rounded-md border border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg text-[12px] px-3 py-2">
            {opError}
          </div>
        )}

        {canEdit && (
          <div className="flex items-center justify-between gap-3 pt-1">
            <div className="text-[11.5px] text-ink-faint">
              {savedFlash && <span className="text-status-ok-fg">{t("tls.toast.saved")}</span>}
            </div>
            <div className="flex items-center gap-2">
              {dirty && (
                <button
                  onClick={() => setEnabled(initialEnabled)}
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
