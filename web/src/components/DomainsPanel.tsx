import { Alert, Badge, Button, Card, CardBody, CardHeader, Input, Label, Table, TBody, TD, TH, THead, TR, confirmDialog } from "./ui";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
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
        if (err.code === "limit_reached") return setOpError(t("domains.error.limit_reached"));
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

  const defaultHost = q.data?.default?.hostname ?? "";
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

  async function onRemove(hostname: string) {
    if (!(await confirmDialog({ title: t("domains.remove.confirm", { hostname }), danger: true }))) return;
    remove.mutate(hostname);
  }

  const customs = q.data?.custom ?? [];
  const maxCustom = q.data?.max_custom ?? 1;
  const atLimit = customs.length >= maxCustom;

  return (
    <Card>
      <CardHeader
        title={t("domains.heading")}
        description={t("domains.description", { default: defaultHost || "<slug>.example.com" })}
      />

      <CardBody>
        <Table bare>
          <THead>
            <tr>
              <TH>{t("domains.col.hostname")}</TH>
              <TH>{t("domains.col.kind")}</TH>
              <TH>{t("domains.col.added")}</TH>
              <TH className="text-right">{t("domains.col.actions")}</TH>
            </tr>
          </THead>
          <TBody>
            {/* Default subdomain — never editable. Always at top. */}
            {q.data && (
              <TR>
                <TD className="font-mono text-[12.5px] text-ink-strong">
                  <a
                    href={`https://${defaultHost}`}
                    target="_blank"
                    rel="noreferrer"
                    className="hover:text-ink-strong hover:underline inline-flex items-center gap-1"
                  >
                    {defaultHost}
                    <ExternalIcon className="size-3 opacity-60" />
                  </a>
                </TD>
                <TD>
                  <Badge variant="solid">{t("domains.default.tag")}</Badge>
                </TD>
                <TD className="text-ink-faint">—</TD>
                <TD className="text-right text-ink-faint">—</TD>
              </TR>
            )}
            {customs.map((d) => (
              <TR key={d.id}>
                <TD className="font-mono text-[12.5px] text-ink-strong">
                  <a
                    href={`https://${d.hostname}`}
                    target="_blank"
                    rel="noreferrer"
                    className="hover:text-ink-strong hover:underline inline-flex items-center gap-1"
                  >
                    {d.hostname}
                    <ExternalIcon className="size-3 opacity-60" />
                  </a>
                </TD>
                <TD className="text-ink-muted">{t("domains.kind.custom")}</TD>
                <TD className="text-ink-muted tabular-nums">{timeAgo(d.created_at)}</TD>
                <TD className="text-right">
                  {canEdit && (
                    <Button
                      variant="destructive-ghost"
                      size="sm"
                      onClick={() => onRemove(d.hostname)}
                      disabled={remove.isPending}
                    >
                      ✕
                    </Button>
                  )}
                </TD>
              </TR>
            ))}
            {q.data && customs.length === 0 && (
              <tr>
                <TD colSpan={4} className="text-[12.5px] text-ink-faint italic">
                  {t("domains.empty")}
                </TD>
              </tr>
            )}
          </TBody>
        </Table>

        {canEdit && atLimit && (
          <Alert className="mt-5">{t("domains.limit.reached", { max: String(maxCustom) })}</Alert>
        )}

        {canEdit && !atLimit && (
          <div className="mt-5 space-y-5">
            {/* Vanity subdomain — fixed suffix, only the prefix is editable.
                No DNS / TLS for the user to set up; wildcard cert covers it. */}
            <form onSubmit={onAddSubdomain} className="space-y-2">
              <Label className="block text-[12px] text-ink-muted">{t("domains.subdomain.heading")}</Label>
              <div className="flex items-stretch gap-2">
                <div className="flex-1 inline-flex h-9 rounded-md border border-line bg-canvas-base overflow-hidden transition-[border-color,box-shadow] hover:border-line-strong focus-within:border-line-strong focus-within:ring-[3px] focus-within:ring-ink-strong/10">
                  <Input
                    value={subInput}
                    onChange={(e) => setSubInput(e.target.value)}
                    placeholder={t("domains.subdomain.placeholder")}
                    spellCheck={false}
                    autoComplete="off"
                    className="h-full min-w-0 flex-1 rounded-none border-0 bg-transparent font-mono focus-visible:ring-0"
                  />
                  <span className="inline-flex items-center px-3 text-[12.5px] font-mono text-ink-muted bg-canvas-surface border-l border-line whitespace-nowrap">
                    .{baseDomain || "example.com"}
                  </span>
                </div>
                <Button type="submit" className="h-9" disabled={add.isPending || subInput.trim() === ""}>
                  {add.isPending ? t("domains.add.busy") : t("domains.subdomain.button")}
                </Button>
              </div>
              <p className="text-[11.5px] text-ink-muted leading-relaxed">
                {t("domains.subdomain.hint", { base: baseDomain || "example.com" })}
              </p>
            </form>

            {/* Fully-custom domain — user owns the DNS, types the full FQDN. */}
            <form onSubmit={onAddCustom} className="space-y-2">
              <Label className="block text-[12px] text-ink-muted">{t("domains.custom.heading")}</Label>
              <div className="flex gap-2">
                <Input
                  value={hostInput}
                  onChange={(e) => setHostInput(e.target.value)}
                  placeholder={t("domains.add.placeholder")}
                  spellCheck={false}
                  autoComplete="off"
                  className="flex-1 font-mono"
                />
                <Button type="submit" className="h-9" disabled={add.isPending || hostInput.trim() === ""}>
                  {add.isPending ? t("domains.add.busy") : t("domains.add.button")}
                </Button>
              </div>
              <p className="text-[11.5px] text-ink-muted leading-relaxed">
                {t("domains.custom.hint", { default: defaultHost || "<slug>.example.com" })}
              </p>
            </form>
          </div>
        )}

        {opError && <Alert variant="danger" className="mt-3">{opError}</Alert>}
      </CardBody>
    </Card>
  );
}
