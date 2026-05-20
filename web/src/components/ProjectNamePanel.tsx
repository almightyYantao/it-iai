import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";

// ProjectNamePanel — rename the project's display name. The slug stays fixed
// (touching it would break deploy tokens, ingresses, bookmarks); this is just
// the human-readable label shown in the UI, Skill output, and audit log.
//
// Surfaces the warning case where name == slug — that happens when the Skill
// fell through to its default (e.g. CI / piped Claude run with no TTY). Users
// land on the project page, see "name is the slug", and have an obvious spot
// to fix it.
export function ProjectNamePanel({
  slug,
  initialName,
  canEdit,
}: {
  slug: string;
  initialName: string;
  canEdit: boolean;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();

  const [draft, setDraft] = useState(initialName);
  const [opError, setOpError] = useState<string | null>(null);
  const [savedFlash, setSavedFlash] = useState(false);

  useEffect(() => {
    setDraft(initialName);
  }, [initialName]);

  const trimmed = draft.trim();
  const dirty = trimmed !== initialName && trimmed !== "";
  const isDefaulted = initialName === slug;

  const save = useMutation({
    mutationFn: () => api.setProjectName(slug, trimmed),
    onSuccess: () => {
      setOpError(null);
      setSavedFlash(true);
      setTimeout(() => setSavedFlash(false), 1800);
      qc.invalidateQueries({ queryKey: ["project", slug] });
    },
    onError: (err) => {
      if (err instanceof ApiError) {
        if (err.code === "empty_name") return setOpError(t("name.error.empty"));
        if (err.code === "name_too_long") return setOpError(t("name.error.too_long"));
        return setOpError(err.message);
      }
      setOpError(String(err));
    },
  });

  return (
    <section className="rounded-lg border border-line-subtle bg-canvas-surface">
      <header className="px-5 py-4 border-b border-line-subtle/60">
        <h3 className="text-[14px] font-semibold text-ink-strong">{t("name.title")}</h3>
        <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">
          {t("name.desc")}
        </p>
      </header>

      <div className="px-5 py-4 space-y-3">
        {isDefaulted && (
          <div className="rounded-md border border-status-warn-fg/30 bg-status-warn-bg/40 text-status-warn-fg text-[12px] px-3 py-2 leading-relaxed">
            {t("name.warning.defaulted", { slug })}
          </div>
        )}

        <div className="flex items-center gap-2">
          <input
            type="text"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            disabled={!canEdit || save.isPending}
            spellCheck={false}
            maxLength={120}
            placeholder={t("name.placeholder")}
            className="flex-1 rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand disabled:opacity-60"
          />
          {canEdit && (
            <button
              onClick={() => save.mutate()}
              disabled={!dirty || save.isPending}
              className={cn(
                "px-3.5 py-1.5 rounded-md text-[12.5px] font-medium transition-colors whitespace-nowrap",
                dirty && !save.isPending
                  ? "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT"
                  : "bg-canvas-raised text-ink-faint cursor-not-allowed",
              )}
            >
              {save.isPending ? t("access.button.save.busy") : t("access.button.save")}
            </button>
          )}
        </div>

        <div className="flex items-center justify-between text-[11.5px] text-ink-faint">
          <span>
            {t("name.slug.label")}: <span className="font-mono">{slug}</span> {t("name.slug.fixed")}
          </span>
          {savedFlash && <span className="text-status-ok-fg">{t("access.toast.saved")}</span>}
        </div>

        {opError && (
          <div className="rounded-md border border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg text-[12px] px-3 py-2">
            {opError}
          </div>
        )}
      </div>
    </section>
  );
}
