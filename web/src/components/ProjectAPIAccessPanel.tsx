import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import type { ProjectPathRule } from "../lib/types";

// ProjectAPIAccessPanel — manages the project's API-level escape hatches:
//
//   - A single project-wide API token. Owners regenerate it (plaintext shown
//     once in a modal) or revoke it. Stored as sha256 + 16-char prefix on
//     the server; we never see the plaintext again after generation.
//
//   - A list of path-prefix overrides. Each row says "for requests whose URL
//     starts with this prefix, replace the project's default SSO gate with
//     <mode>". Two modes:
//       no_auth — skip SSO entirely (IP allow-list still applies).
//       token   — require the project API token as Authorization: Bearer.
//
// Longer prefixes win when multiple could match. Read-only for collaborators
// (canEdit = canManageProject upstream).
export function ProjectAPIAccessPanel({
  slug,
  initialTokenPrefix,
  initialTokenCreatedAt,
  canEdit,
}: {
  slug: string;
  initialTokenPrefix?: string | null;
  initialTokenCreatedAt?: string | null;
  canEdit: boolean;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();

  // Plaintext returned by regenerate is held in local state for the modal —
  // never put it in a query cache so it can't accidentally leak via devtools.
  const [revealed, setRevealed] = useState<string | null>(null);
  const [opError, setOpError] = useState<string | null>(null);

  const rulesQ = useQuery({
    queryKey: ["path-rules", slug],
    queryFn: () => api.listProjectPathRules(slug),
  });
  const rules = useMemo<ProjectPathRule[]>(() => rulesQ.data?.rules ?? [], [rulesQ.data]);

  const regenerate = useMutation({
    mutationFn: () => api.regenerateProjectAPIToken(slug),
    onSuccess: (resp) => {
      setOpError(null);
      setRevealed(resp.token);
      qc.invalidateQueries({ queryKey: ["project", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  const revoke = useMutation({
    mutationFn: () => api.revokeProjectAPIToken(slug),
    onSuccess: () => {
      setOpError(null);
      qc.invalidateQueries({ queryKey: ["project", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  // Add-rule form state
  const [newPrefix, setNewPrefix] = useState("");
  const [newMode, setNewMode] = useState<"no_auth" | "token">("no_auth");

  const createRule = useMutation({
    mutationFn: () => api.createProjectPathRule(slug, { path_prefix: newPrefix.trim(), mode: newMode }),
    onSuccess: () => {
      setOpError(null);
      setNewPrefix("");
      qc.invalidateQueries({ queryKey: ["path-rules", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  const deleteRule = useMutation({
    mutationFn: (id: string) => api.deleteProjectPathRule(slug, id),
    onSuccess: () => {
      setOpError(null);
      qc.invalidateQueries({ queryKey: ["path-rules", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  // Detect token-mode rules created when no project token exists — the rule
  // does nothing useful in that state, so we flag it inline.
  const hasToken = Boolean(initialTokenPrefix);
  const tokenModeWithoutToken = rules.some((r) => r.mode === "token") && !hasToken;

  function copyToClipboard(text: string) {
    void navigator.clipboard?.writeText(text);
  }

  return (
    <section className="rounded-lg border border-line-subtle bg-canvas-surface">
      <header className="flex items-start justify-between gap-4 px-5 py-4 border-b border-line-subtle/60">
        <div>
          <h3 className="text-[14px] font-semibold text-ink-strong">{t("apiAccess.title")}</h3>
          <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">
            {t("apiAccess.desc")}
          </p>
        </div>
      </header>

      <div className="px-5 py-4 space-y-5">
        {/* ----- Token section ------------------------------------------- */}
        <div>
          <div className="text-[12.5px] font-medium text-ink-DEFAULT mb-2">
            {t("apiAccess.token.label")}
          </div>
          {hasToken ? (
            <div className="flex items-center gap-3 text-[12.5px]">
              <code className="font-mono text-ink-strong bg-canvas-base border border-line-subtle rounded px-2 py-0.5">
                {initialTokenPrefix}…
              </code>
              <span className="text-ink-faint">
                {t("apiAccess.token.createdAt", {
                  at: initialTokenCreatedAt
                    ? new Date(initialTokenCreatedAt).toLocaleString()
                    : "—",
                })}
              </span>
              {canEdit && (
                <>
                  <button
                    onClick={() => regenerate.mutate()}
                    disabled={regenerate.isPending}
                    className="ml-auto px-2.5 py-1 rounded-md text-[12px] border border-line text-ink-DEFAULT hover:bg-canvas-raised disabled:opacity-60"
                  >
                    {regenerate.isPending ? t("apiAccess.token.btn.regen.busy") : t("apiAccess.token.btn.regen")}
                  </button>
                  <button
                    onClick={() => {
                      if (window.confirm(t("apiAccess.token.confirmRevoke"))) {
                        revoke.mutate();
                      }
                    }}
                    disabled={revoke.isPending}
                    className="px-2.5 py-1 rounded-md text-[12px] border border-line text-status-fail-fg hover:bg-status-fail-bg/40 disabled:opacity-60"
                  >
                    {revoke.isPending ? t("apiAccess.token.btn.revoke.busy") : t("apiAccess.token.btn.revoke")}
                  </button>
                </>
              )}
            </div>
          ) : (
            <div className="flex items-center gap-3 text-[12.5px]">
              <span className="text-ink-faint">{t("apiAccess.token.none")}</span>
              {canEdit && (
                <button
                  onClick={() => regenerate.mutate()}
                  disabled={regenerate.isPending}
                  className="ml-auto px-2.5 py-1 rounded-md text-[12px] bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT disabled:opacity-60"
                >
                  {regenerate.isPending ? t("apiAccess.token.btn.create.busy") : t("apiAccess.token.btn.create")}
                </button>
              )}
            </div>
          )}
        </div>

        {/* ----- Rules section ------------------------------------------- */}
        <div>
          <div className="text-[12.5px] font-medium text-ink-DEFAULT mb-2">
            {t("apiAccess.rules.label")}
          </div>
          {tokenModeWithoutToken && (
            <div className="mb-2 rounded-md border border-status-warn-fg/30 bg-status-warn-bg/30 text-status-warn-fg text-[12px] px-3 py-2">
              {t("apiAccess.rules.warn.tokenButNoSecret")}
            </div>
          )}
          {rulesQ.isLoading && (
            <div className="text-[12.5px] text-ink-faint">{t("apiAccess.rules.loading")}</div>
          )}
          {!rulesQ.isLoading && rules.length === 0 && (
            <div className="text-[12.5px] text-ink-faint">{t("apiAccess.rules.empty")}</div>
          )}
          {rules.length > 0 && (
            <ul className="divide-y divide-line-subtle/60 border border-line-subtle rounded-md overflow-hidden">
              {rules.map((r) => (
                <li key={r.id} className="flex items-center gap-3 px-3 py-2 text-[12.5px]">
                  <code className="font-mono text-ink-strong">{r.path_prefix}</code>
                  <span
                    className={cn(
                      "inline-flex items-center rounded-full px-2 py-0.5 text-[10.5px] font-medium tracking-wide uppercase",
                      r.mode === "no_auth"
                        ? "bg-canvas-raised text-ink-muted"
                        : "bg-brand/12 text-brand",
                    )}
                  >
                    {r.mode === "no_auth" ? t("apiAccess.rules.mode.no_auth") : t("apiAccess.rules.mode.token")}
                  </span>
                  {canEdit && (
                    <button
                      onClick={() => deleteRule.mutate(r.id)}
                      disabled={deleteRule.isPending}
                      className="ml-auto text-[11.5px] text-ink-faint hover:text-status-fail-fg transition-colors"
                    >
                      {t("apiAccess.rules.btn.delete")}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}

          {canEdit && (
            <div className="mt-3 flex items-center gap-2">
              <input
                type="text"
                value={newPrefix}
                onChange={(e) => setNewPrefix(e.target.value)}
                placeholder={t("apiAccess.rules.input.placeholder")}
                className="flex-1 min-w-0 rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] font-mono text-ink-strong focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
              />
              <select
                value={newMode}
                onChange={(e) => setNewMode(e.target.value as "no_auth" | "token")}
                className="rounded-md border border-line bg-canvas-base px-2 py-1.5 text-[13px] text-ink-strong focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
              >
                <option value="no_auth">{t("apiAccess.rules.mode.no_auth")}</option>
                <option value="token">{t("apiAccess.rules.mode.token")}</option>
              </select>
              <button
                onClick={() => createRule.mutate()}
                disabled={createRule.isPending || !newPrefix.trim()}
                className={cn(
                  "px-3 py-1.5 rounded-md text-[12.5px] font-medium transition-colors shrink-0",
                  createRule.isPending || !newPrefix.trim()
                    ? "bg-canvas-raised text-ink-faint cursor-not-allowed"
                    : "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT",
                )}
              >
                {createRule.isPending ? t("apiAccess.rules.btn.add.busy") : t("apiAccess.rules.btn.add")}
              </button>
            </div>
          )}
        </div>

        {opError && (
          <div className="rounded-md border border-status-fail-fg/30 bg-status-fail-bg/40 text-status-fail-fg text-[12px] px-3 py-2">
            {opError}
          </div>
        )}
      </div>

      {/* Reveal modal — shown exactly once when a new token is minted */}
      {revealed && (
        <RevealModal
          token={revealed}
          onCopy={() => copyToClipboard(revealed)}
          onClose={() => setRevealed(null)}
        />
      )}
    </section>
  );
}

function RevealModal({
  token,
  onCopy,
  onClose,
}: {
  token: string;
  onCopy: () => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(id);
  }, [copied]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg rounded-lg border border-line bg-canvas-surface shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-5 py-4 border-b border-line-subtle/60">
          <h4 className="text-[14px] font-semibold text-ink-strong">{t("apiAccess.reveal.title")}</h4>
          <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed">
            {t("apiAccess.reveal.desc")}
          </p>
        </div>
        <div className="px-5 py-4 space-y-3">
          <code className="block font-mono text-[12px] text-ink-strong bg-canvas-base border border-line-subtle rounded px-3 py-2 break-all">
            {token}
          </code>
          <div className="flex items-center justify-end gap-2">
            <button
              onClick={() => {
                onCopy();
                setCopied(true);
              }}
              className="px-3 py-1.5 rounded-md text-[12.5px] border border-line text-ink-DEFAULT hover:bg-canvas-raised"
            >
              {copied ? t("apiAccess.reveal.copied") : t("apiAccess.reveal.copy")}
            </button>
            <button
              onClick={onClose}
              className="px-3 py-1.5 rounded-md text-[12.5px] bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT"
            >
              {t("apiAccess.reveal.close")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
