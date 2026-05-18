import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { ErrorState } from "./States";
import { LoadingRows } from "./States";
import { OwnerCell } from "./Owner";
import { timeAgo } from "../lib/format";

export function CollaboratorsPanel({ slug, ownerEmail }: { slug: string; ownerEmail?: string }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [opError, setOpError] = useState<string | null>(null);

  const q = useQuery({
    queryKey: ["collaborators", slug],
    queryFn: () => api.listCollaborators(slug),
  });

  const add = useMutation({
    mutationFn: (e: string) => api.addCollaborator(slug, e),
    onSuccess: () => {
      setEmail("");
      setOpError(null);
      qc.invalidateQueries({ queryKey: ["collaborators", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  const remove = useMutation({
    mutationFn: (e: string) => api.removeCollaborator(slug, e),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collaborators", slug] }),
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  function onAdd(e: React.FormEvent) {
    e.preventDefault();
    const v = email.trim();
    if (!v) return;
    add.mutate(v);
  }

  function onRemove(e: string) {
    if (!window.confirm(t("collab.remove.confirm", { email: e }))) return;
    remove.mutate(e);
  }

  return (
    <section>
      <h2 className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium mb-2">
        {t("collab.heading")}
      </h2>
      <p className="text-[12.5px] text-ink-muted mb-3">
        {t("collab.description", { owner: ownerEmail ?? t("common.empty") })}
      </p>

      <form onSubmit={onAdd} className="flex gap-2 mb-3">
        <input
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder={t("collab.add.placeholder")}
          className="flex-1 h-9 rounded-md bg-canvas-surface border border-line px-3 text-[13px] placeholder-ink-faint text-ink-DEFAULT focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand-ring transition"
        />
        <button
          type="submit"
          disabled={add.isPending || !email.trim()}
          className="h-9 rounded-md bg-brand hover:bg-brand-hover disabled:opacity-50 px-4 text-[13px] font-medium text-canvas-base transition"
        >
          {t("collab.add.button")}
        </button>
      </form>

      {opError && (
        <div className="mb-3">
          <ErrorState message={opError} />
        </div>
      )}

      {q.isLoading && <LoadingRows rows={2} cols={3} />}
      {q.error && <ErrorState message={(q.error as Error).message} />}

      {q.data && (q.data.collaborators?.length ?? 0) === 0 && (
        <div className="rounded-md border border-dashed border-line bg-canvas-surface px-5 py-6 text-center text-[13px] text-ink-muted">
          {t("collab.empty", { owner: ownerEmail ?? t("common.empty") })}
        </div>
      )}

      {q.data && (q.data.collaborators?.length ?? 0) > 0 && (
        <div className="overflow-hidden rounded-lg border border-line-subtle bg-canvas-surface">
          <table className="min-w-full text-[13px]">
            <thead className="bg-canvas-base/40">
              <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                <th className="px-5 py-3 font-medium">{t("collab.col.email")}</th>
                <th className="px-5 py-3 font-medium">{t("collab.col.role")}</th>
                <th className="px-5 py-3 font-medium">{t("collab.col.added")}</th>
                <th className="px-5 py-3 font-medium text-right">{t("users.col.actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-subtle/60">
              {q.data.collaborators.map((c) => (
                <tr key={c.user_id} className="hover:bg-canvas-raised/50 transition-colors">
                  <td className="px-5 py-3"><OwnerCell email={c.email} /></td>
                  <td className="px-5 py-3 text-ink-muted">
                    {c.role === "admin" ? t("collab.role.admin") : t("collab.role.editor")}
                  </td>
                  <td className="px-5 py-3 text-ink-muted tabular-nums">{timeAgo(c.added_at)}</td>
                  <td className="px-5 py-3 text-right">
                    <button
                      onClick={() => onRemove(c.email)}
                      className="text-[12px] text-ink-faint hover:text-status-fail-fg transition-colors"
                    >
                      {t("common.remove")}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
