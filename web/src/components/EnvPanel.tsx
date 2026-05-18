import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";

// Server-side validator mirror (see internal/api/project_env.go).
// Catch typos in the form before round-tripping.
const KEY_RE = /^[A-Za-z_][A-Za-z0-9_]{0,127}$/;

// EnvPanel — encrypted env vars for a single project.
//
// Read model: GET returns metadata (key + audit), never values. A user who
// wants to know what's set sees the key list; they can re-PUT to rotate but
// can't read the current value. This is intentional — it prevents accidental
// "look over my shoulder" leaks during demos and removes the "old token still
// in DB" footgun.
//
// Platform-managed rows (`system: true`) render with a badge and no Delete
// button. Editing one would just get rejected by the server with a 409.
export function EnvPanel({
  slug,
  canEdit,
}: {
  slug: string;
  canEdit: boolean;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [keyInput, setKeyInput] = useState("");
  const [valueInput, setValueInput] = useState("");
  const [opError, setOpError] = useState<string | null>(null);

  const q = useQuery({
    queryKey: ["project-env", slug],
    queryFn: () => api.listProjectEnv(slug),
  });

  const save = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) =>
      api.setProjectEnv(slug, key, value),
    onSuccess: () => {
      setKeyInput("");
      setValueInput("");
      setOpError(null);
      qc.invalidateQueries({ queryKey: ["project-env", slug] });
    },
    onError: (err) => {
      if (err instanceof ApiError) {
        if (err.code === "bad_key")     return setOpError(t("env.error.invalid"));
        if (err.code === "system_env")  return setOpError(t("env.error.system"));
        return setOpError(err.message);
      }
      setOpError(String(err));
    },
  });

  const remove = useMutation({
    mutationFn: (key: string) => api.deleteProjectEnv(slug, key),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["project-env", slug] }),
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  function onSave(e: React.FormEvent) {
    e.preventDefault();
    const k = keyInput.trim();
    if (!KEY_RE.test(k)) {
      setOpError(t("env.error.invalid"));
      return;
    }
    save.mutate({ key: k, value: valueInput });
  }

  function onRemove(key: string) {
    if (!confirm(t("env.remove.confirm", { key }))) return;
    remove.mutate(key);
  }

  const rows = q.data?.env ?? [];

  return (
    <section className="rounded-lg border border-line-subtle bg-canvas-surface">
      <header className="px-5 py-4 border-b border-line-subtle/60">
        <h3 className="text-[14px] font-semibold text-ink-strong">{t("env.heading")}</h3>
        <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">
          {t("env.description")}
        </p>
      </header>

      <div className="px-5 py-4">
        <table className="min-w-full text-[13px]">
          <thead>
            <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
              <th className="py-2 pr-4 font-medium">{t("env.col.key")}</th>
              <th className="py-2 pr-4 font-medium">{t("env.col.updated")}</th>
              <th className="py-2 font-medium text-right">{t("env.col.actions")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line-subtle/60">
            {rows.map((e) => (
              <tr key={e.key} className="align-middle">
                <td className="py-3 pr-4">
                  <span className="font-mono text-[12.5px] text-ink-strong">{e.key}</span>
                  {e.system && (
                    <span
                      className="ml-2 inline-flex items-center rounded-full bg-canvas-raised text-ink-muted px-1.5 py-0.5 text-[10.5px] font-medium tracking-wide uppercase"
                      title={t("env.system.hint")}
                    >
                      {t("env.system.tag")}
                    </span>
                  )}
                </td>
                <td className="py-3 pr-4 text-ink-muted tabular-nums">{timeAgo(e.updated_at)}</td>
                <td className="py-3 text-right">
                  {canEdit && !e.system && (
                    <button
                      onClick={() => onRemove(e.key)}
                      disabled={remove.isPending}
                      className="text-[12px] text-status-fail-fg hover:underline"
                    >
                      ✕
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {q.data && rows.length === 0 && (
              <tr>
                <td colSpan={3} className="py-4 text-[12.5px] text-ink-faint italic">
                  {t("env.empty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>

        {canEdit && (
          <form onSubmit={onSave} className="mt-5 space-y-2">
            <div className="flex gap-2">
              <input
                value={keyInput}
                onChange={(e) => setKeyInput(e.target.value)}
                placeholder={t("env.add.key.placeholder")}
                spellCheck={false}
                autoComplete="off"
                className="w-44 rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
              />
              <input
                value={valueInput}
                onChange={(e) => setValueInput(e.target.value)}
                placeholder={t("env.add.value.placeholder")}
                type="password"
                spellCheck={false}
                autoComplete="new-password"
                className="flex-1 rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
              />
              <button
                type="submit"
                disabled={save.isPending || keyInput.trim() === ""}
                className={cn(
                  "px-3.5 py-1.5 rounded-md text-[12.5px] font-medium transition-colors",
                  save.isPending || keyInput.trim() === ""
                    ? "bg-canvas-raised text-ink-faint cursor-not-allowed"
                    : "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT",
                )}
              >
                {save.isPending ? t("env.add.busy") : t("env.add.button")}
              </button>
            </div>
            <p className="text-[11.5px] text-ink-muted leading-relaxed">{t("env.add.hint")}</p>
          </form>
        )}

        {opError && (
          <div className="mt-3 rounded-md border border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg text-[12px] px-3 py-2">
            {opError}
          </div>
        )}
      </div>
    </section>
  );
}
