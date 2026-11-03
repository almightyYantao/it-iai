import { Alert, Badge, Button, Card, CardBody, CardHeader, Input, Select, confirmDialog } from "./ui";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
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
    <Card>
      <CardHeader title={t("apiAccess.title")} description={t("apiAccess.desc")} />

      <CardBody className="space-y-5">
        {/* ----- Token section ------------------------------------------- */}
        <div>
          <div className="text-[12.5px] font-medium text-ink mb-2">
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
                  <Button
                    variant="secondary"
                    size="sm"
                    className="ml-auto"
                    onClick={() => regenerate.mutate()}
                    disabled={regenerate.isPending}
                  >
                    {regenerate.isPending ? t("apiAccess.token.btn.regen.busy") : t("apiAccess.token.btn.regen")}
                  </Button>
                  <Button
                    variant="destructive-ghost"
                    size="sm"
                    onClick={async () => {
                      if (await confirmDialog({ title: t("apiAccess.token.confirmRevoke"), danger: true })) {
                        revoke.mutate();
                      }
                    }}
                    disabled={revoke.isPending}
                  >
                    {revoke.isPending ? t("apiAccess.token.btn.revoke.busy") : t("apiAccess.token.btn.revoke")}
                  </Button>
                </>
              )}
            </div>
          ) : (
            <div className="flex items-center gap-3 text-[12.5px]">
              <span className="text-ink-faint">{t("apiAccess.token.none")}</span>
              {canEdit && (
                <Button size="sm" className="ml-auto" onClick={() => regenerate.mutate()} disabled={regenerate.isPending}>
                  {regenerate.isPending ? t("apiAccess.token.btn.create.busy") : t("apiAccess.token.btn.create")}
                </Button>
              )}
            </div>
          )}
        </div>

        {/* ----- Rules section ------------------------------------------- */}
        <div>
          <div className="text-[12.5px] font-medium text-ink mb-2">
            {t("apiAccess.rules.label")}
          </div>
          {tokenModeWithoutToken && (
            <Alert variant="warning" className="mb-2">{t("apiAccess.rules.warn.tokenButNoSecret")}</Alert>
          )}
          {rulesQ.isLoading && (
            <div className="text-[12.5px] text-ink-faint">{t("apiAccess.rules.loading")}</div>
          )}
          {!rulesQ.isLoading && rules.length === 0 && (
            <div className="text-[12.5px] text-ink-faint">{t("apiAccess.rules.empty")}</div>
          )}
          {rules.length > 0 && (
            <ul className="divide-y divide-line-subtle border border-line-subtle rounded-md overflow-hidden">
              {rules.map((r) => (
                <li key={r.id} className="flex items-center gap-3 px-3 py-2 text-[12.5px]">
                  <code className="font-mono text-ink-strong">{r.path_prefix}</code>
                  <Badge variant={r.mode === "no_auth" ? "default" : "info"}>
                    {r.mode === "no_auth" ? t("apiAccess.rules.mode.no_auth") : t("apiAccess.rules.mode.token")}
                  </Badge>
                  {canEdit && (
                    <Button
                      variant="destructive-ghost"
                      size="sm"
                      className="ml-auto"
                      onClick={() => deleteRule.mutate(r.id)}
                      disabled={deleteRule.isPending}
                    >
                      {t("apiAccess.rules.btn.delete")}
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}

          {canEdit && (
            <div className="mt-3 flex items-center gap-2">
              <Input
                type="text"
                value={newPrefix}
                onChange={(e) => setNewPrefix(e.target.value)}
                placeholder={t("apiAccess.rules.input.placeholder")}
                className="flex-1 min-w-0 font-mono"
              />
              <Select className="w-40" value={newMode} onChange={setNewMode}
                options={[{ value: "no_auth", label: t("apiAccess.rules.mode.no_auth") }, { value: "token", label: t("apiAccess.rules.mode.token") }]} />
              <Button className="h-9" onClick={() => createRule.mutate()} disabled={createRule.isPending || !newPrefix.trim()}>
                {createRule.isPending ? t("apiAccess.rules.btn.add.busy") : t("apiAccess.rules.btn.add")}
              </Button>
            </div>
          )}
        </div>

        {opError && <Alert variant="danger">{opError}</Alert>}
      </CardBody>

      {/* Reveal modal — shown exactly once when a new token is minted */}
      {revealed && (
        <RevealModal
          token={revealed}
          onCopy={() => copyToClipboard(revealed)}
          onClose={() => setRevealed(null)}
        />
      )}
    </Card>
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
            <Button
              variant="secondary"
              onClick={() => {
                onCopy();
                setCopied(true);
              }}
            >
              {copied ? t("apiAccess.reveal.copied") : t("apiAccess.reveal.copy")}
            </Button>
            <Button onClick={onClose}>{t("apiAccess.reveal.close")}</Button>
          </div>
        </div>
      </div>
    </div>
  );
}
