import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";
import { ExternalIcon } from "./icons";

// Same shape the server uses to validate (see internal/api/domains.go).
// Keeping it client-side too avoids a roundtrip just to tell the user they
// typed a space.
const HOST_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$/;

// DomainsPanel — manage custom hostnames for a project.
//
// Add: validates locally (basic shape) before hitting the server, surfaces the
// server's structured errors (taken / reserved / invalid) as human messages.
// List: server returns `default` (read-only platform subdomain) + `custom`
// (the editable rows). The default subdomain always renders at the top with
// a "Default" tag so users see what URL their project is on without having
// to scroll.
// Single DNS-label regex for the vanity-subdomain input (no dots allowed —
// "app.foo" would create a deeper hostname that the wildcard cert doesn't cover,
// so we reject early instead of round-tripping to the server).
const LABEL_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

export function DomainsPanel({
  slug,
  canEdit,
}: {
  slug: string;
  canEdit: boolean;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [hostInput, setHostInput] = useState("");
  const [subInput, setSubInput] = useState("");
  const [opError, setOpError] = useState<string | null>(null);

  const q = useQuery({
    queryKey: ["domains", slug],
    queryFn: () => api.listDomains(slug),
  });

  const add = useMutation({
    mutationFn: (hostname: string) => api.addDomain(slug, hostname),
    onSuccess: () => {
      setHostInput("");
      setSubInput("");
      setOpError(null);
      qc.invalidateQueries({ queryKey: ["domains", slug] });
    },
    onError: (err) => {
      // Map the server's error codes (see writeError calls in handleAddDomain)
      // to concrete UI messages. Fall back to the raw message for the long
      // tail (e.g. DB outage).
      if (err instanceof ApiError) {
        if (err.code === "hostname_taken") return setOpError(t("domains.error.taken"));
        if (err.code === "bad_hostname")  return setOpError(t("domains.error.invalid"));
        if (err.code === "reserved")      return setOpError(t("domains.error.reserved"));
        return setOpError(err.message);
      }
      setOpError(String(err));
    },
  });

  const remove = useMutation({
    mutationFn: (hostname: string) => api.removeDomain(slug, hostname),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["domains", slug] }),
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  const defaultHost = q.data?.default.hostname ?? "";
  // The platform base domain = everything after the slug. The default hostname
  // is always "<slug>.<base>" by construction, so this split is safe.
  const baseDomain = defaultHost.startsWith(slug + ".")
    ? defaultHost.slice(slug.length + 1)
    : "";

  function onAddSubdomain(e: React.FormEvent) {
    e.preventDefault();
    const prefix = subInput.trim().toLowerCase();
    if (!LABEL_RE.test(prefix)) {
      setOpError(t("domains.error.invalid"));
      return;
    }
    if (!baseDomain) {
      setOpError(t("domains.error.invalid"));
      return;
    }
    add.mutate(`${prefix}.${baseDomain}`);
  }

  function onAddCustom(e: React.FormEvent) {
    e.preventDefault();
    const v = hostInput.trim().toLowerCase();
    if (!HOST_RE.test(v)) {
      setOpError(t("domains.error.invalid"));
      return;
    }
    add.mutate(v);
  }

  function onRemove(hostname: string) {
    if (!confirm(t("domains.remove.confirm", { hostname }))) return;
    remove.mutate(hostname);
  }

  const customs = q.data?.custom ?? [];

  return (
    <section className="rounded-lg border border-line-subtle bg-canvas-surface">
      <header className="px-5 py-4 border-b border-line-subtle/60">
        <h3 className="text-[14px] font-semibold text-ink-strong">{t("domains.heading")}</h3>
        <p className="mt-1 text-[12.5px] text-ink-muted leading-relaxed max-w-prose">
          {t("domains.description", { default: defaultHost || "<slug>.example.com" })}
        </p>
      </header>

      <div className="px-5 py-4">
        <table className="min-w-full text-[13px]">
          <thead>
            <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
              <th className="py-2 pr-4 font-medium">{t("domains.col.hostname")}</th>
              <th className="py-2 pr-4 font-medium">{t("domains.col.kind")}</th>
              <th className="py-2 pr-4 font-medium">{t("domains.col.added")}</th>
              <th className="py-2 font-medium text-right">{t("domains.col.actions")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line-subtle/60">
            {/* Default subdomain — never editable. Always at top. */}
            {q.data && (
              <tr className="align-middle">
                <td className="py-3 pr-4 font-mono text-[12.5px] text-ink-strong">
                  <a
                    href={`https://${defaultHost}`}
                    target="_blank"
                    rel="noreferrer"
                    className="hover:text-brand inline-flex items-center gap-1"
                  >
                    {defaultHost}
                    <ExternalIcon className="size-3 opacity-60" />
                  </a>
                </td>
                <td className="py-3 pr-4">
                  <span className="inline-flex items-center rounded-full bg-brand/10 text-brand px-1.5 py-0.5 text-[10.5px] font-medium tracking-wide uppercase">
                    {t("domains.default.tag")}
                  </span>
                </td>
                <td className="py-3 pr-4 text-ink-faint">—</td>
                <td className="py-3 text-right text-ink-faint">—</td>
              </tr>
            )}
            {customs.map((d) => (
              <tr key={d.id} className="align-middle">
                <td className="py-3 pr-4 font-mono text-[12.5px] text-ink-strong">
                  <a
                    href={`https://${d.hostname}`}
                    target="_blank"
                    rel="noreferrer"
                    className="hover:text-brand inline-flex items-center gap-1"
                  >
                    {d.hostname}
                    <ExternalIcon className="size-3 opacity-60" />
                  </a>
                </td>
                <td className="py-3 pr-4 text-ink-muted">{t("domains.kind.custom")}</td>
                <td className="py-3 pr-4 text-ink-muted tabular-nums">{timeAgo(d.created_at)}</td>
                <td className="py-3 text-right">
                  {canEdit && (
                    <button
                      onClick={() => onRemove(d.hostname)}
                      disabled={remove.isPending}
                      className="text-[12px] text-status-fail-fg hover:underline"
                    >
                      ✕
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {q.data && customs.length === 0 && (
              <tr>
                <td colSpan={4} className="py-4 text-[12.5px] text-ink-faint italic">
                  {t("domains.empty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>

        {canEdit && (
          <div className="mt-5 space-y-5">
            {/* Vanity subdomain — fixed suffix, only the prefix is editable.
                No DNS / TLS for the user to set up; wildcard cert covers it. */}
            <form onSubmit={onAddSubdomain} className="space-y-2">
              <div className="text-[11.5px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                {t("domains.subdomain.heading")}
              </div>
              <div className="flex items-stretch gap-2">
                <div className="flex-1 inline-flex rounded-md border border-line bg-canvas-base overflow-hidden focus-within:ring-2 focus-within:ring-brand/30 focus-within:border-brand">
                  <input
                    value={subInput}
                    onChange={(e) => setSubInput(e.target.value)}
                    placeholder={t("domains.subdomain.placeholder")}
                    spellCheck={false}
                    autoComplete="off"
                    className="flex-1 min-w-0 bg-transparent px-3 py-1.5 text-[13px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none"
                  />
                  <span className="px-3 py-1.5 text-[12.5px] font-mono text-ink-muted bg-canvas-raised/40 border-l border-line whitespace-nowrap">
                    .{baseDomain || "example.com"}
                  </span>
                </div>
                <button
                  type="submit"
                  disabled={add.isPending || subInput.trim() === ""}
                  className={cn(
                    "px-3.5 py-1.5 rounded-md text-[12.5px] font-medium transition-colors",
                    add.isPending || subInput.trim() === ""
                      ? "bg-canvas-raised text-ink-faint cursor-not-allowed"
                      : "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT",
                  )}
                >
                  {add.isPending ? t("domains.add.busy") : t("domains.subdomain.button")}
                </button>
              </div>
              <p className="text-[11.5px] text-ink-muted leading-relaxed">
                {t("domains.subdomain.hint", { base: baseDomain || "example.com" })}
              </p>
            </form>

            {/* Fully-custom domain — user owns the DNS, types the full FQDN. */}
            <form onSubmit={onAddCustom} className="space-y-2">
              <div className="text-[11.5px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                {t("domains.custom.heading")}
              </div>
              <div className="flex gap-2">
                <input
                  value={hostInput}
                  onChange={(e) => setHostInput(e.target.value)}
                  placeholder={t("domains.add.placeholder")}
                  spellCheck={false}
                  autoComplete="off"
                  className="flex-1 rounded-md border border-line bg-canvas-base px-3 py-1.5 text-[13px] font-mono text-ink-strong placeholder:text-ink-faint focus:outline-none focus:ring-2 focus:ring-brand/30 focus:border-brand"
                />
                <button
                  type="submit"
                  disabled={add.isPending || hostInput.trim() === ""}
                  className={cn(
                    "px-3.5 py-1.5 rounded-md text-[12.5px] font-medium transition-colors",
                    add.isPending || hostInput.trim() === ""
                      ? "bg-canvas-raised text-ink-faint cursor-not-allowed"
                      : "bg-ink-strong text-canvas-base hover:bg-ink-DEFAULT",
                  )}
                >
                  {add.isPending ? t("domains.add.busy") : t("domains.add.button")}
                </button>
              </div>
              <p className="text-[11.5px] text-ink-muted leading-relaxed">
                {t("domains.custom.hint", { default: defaultHost || "<slug>.example.com" })}
              </p>
            </form>
          </div>
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
