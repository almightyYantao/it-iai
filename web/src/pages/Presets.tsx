import { confirmDialog } from "../components/ui/confirm";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";
import { PageContainer, PageHeader } from "../components/Page";
import { ErrorState, LoadingRows } from "../components/States";
import { Alert, Badge, Button, Card, CardBody, Input, Textarea } from "../components/ui";
import type { CIDRPreset } from "../lib/types";

// Access presets: card list → one-entry-per-row editor with notes, inline
// validation, and a diff confirmation before saving (a save re-syncs every
// project that uses the preset).

const SLUG_RE = /^[a-z0-9][a-z0-9_-]{2,29}$/;
const IPV4 = /^(\d{1,3})(\.\d{1,3}){3}(\/\d{1,2})?$/;
const IPV6 = /^[0-9a-f:]+(\/\d{1,3})?$/i;

type Row = { id: number; cidr: string; note: string };
let rid = 0;

// Mirrors the server's normaliseCIDR so the diff matches what gets stored.
function norm(raw: string): string | null {
  const c = raw.trim();
  if (!c) return null;
  if (IPV4.test(c)) {
    const [ip, bits] = c.split("/");
    if (ip.split(".").some((o) => Number(o) > 255)) return null;
    if (bits !== undefined && Number(bits) > 32) return null;
    return bits === undefined ? `${ip}/32` : c;
  }
  if (c.includes(":") && IPV6.test(c)) return c.includes("/") ? c.toLowerCase() : `${c.toLowerCase()}/128`;
  return null;
}
const isWorld = (c: string) => /\/0$/.test(c);

export function Presets() {
  const { t } = useI18n();
  const q = useQuery({ queryKey: ["cidr-presets"], queryFn: api.listCIDRPresets });
  const [editing, setEditing] = useState<CIDRPreset | "new" | null>(null);
  const presets = (q.data?.presets ?? []).filter((p) => p.name !== "public");

  if (editing) {
    return (
      <PageContainer>
        <PresetEditor preset={editing === "new" ? null : editing} onClose={() => setEditing(null)} />
      </PageContainer>
    );
  }

  return (
    <PageContainer>
      <PageHeader
        title={t("pp.title")}
        description={t("pp.desc")}
        actions={
          <Button onClick={() => setEditing("new")}>
            + {t("pp.new")}
          </Button>
        }
      />
      {q.error && <ErrorState message={(q.error as Error).message} onRetry={() => q.refetch()} />}
      {q.isLoading && <LoadingRows rows={2} cols={3} />}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {presets.map((p) => (
          <Button
            key={p.name}
            variant="secondary"
            onClick={() => setEditing(p)}
            className="group block h-auto whitespace-normal rounded-lg border-line-subtle p-5 text-left font-normal transition-all hover:border-line-strong hover:bg-canvas-base hover:shadow-raised"
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-[15px] font-semibold text-ink-strong">{p.label}</span>
                  {p.is_system && (
                    <Badge>{t("pp.system")}</Badge>
                  )}
                </div>
                <div className="mt-0.5 font-mono text-[12px] text-ink-faint">{p.name}</div>
              </div>
              <span className="text-[12.5px] text-ink-muted group-hover:text-ink-strong">{t("pp.edit")} →</span>
            </div>
            {p.description && <p className="mt-3 line-clamp-2 text-[13px] text-ink-muted">{p.description}</p>}
            <div className="mt-5 grid grid-cols-3 gap-3 border-t border-line-subtle pt-4">
              <Stat label={t("pp.stat.ips")} value={p.cidrs.length} />
              <Stat label={t("pp.stat.projects")} value={p.project_count ?? 0} />
              <Stat label={t("pp.stat.updated")} value={timeAgo(p.updated_at)} small />
            </div>
          </Button>
        ))}
      </div>
    </PageContainer>
  );
}

function Stat({ label, value, small }: { label: string; value: React.ReactNode; small?: boolean }) {
  return (
    <div>
      <div className="text-[12px] text-ink-muted">{label}</div>
      <div className={cn("mt-1 font-semibold tabular-nums text-ink-strong", small ? "text-[13px]" : "text-[20px]")}>{value}</div>
    </div>
  );
}

function PresetEditor({ preset, onClose }: { preset: CIDRPreset | null; onClose: () => void }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const isNew = !preset;
  const [name, setName] = useState(preset?.name ?? "");
  const [label, setLabel] = useState(preset?.label ?? "");
  const [desc, setDesc] = useState(preset?.description ?? "");
  const [rows, setRows] = useState<Row[]>(() =>
    (preset?.cidrs ?? []).map((c) => ({ id: rid++, cidr: c, note: preset?.notes?.[c] ?? "" })),
  );
  const [filter, setFilter] = useState("");
  const [paste, setPaste] = useState("");
  const [showPaste, setShowPaste] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Per-row problems: invalid, world-open, duplicate.
  const problems = useMemo(() => {
    const seen = new Map<string, number>();
    const out = new Map<number, string>();
    for (const r of rows) {
      if (!r.cidr.trim()) continue;
      const n = norm(r.cidr);
      if (!n) out.set(r.id, t("pp.err.invalid"));
      else if (isWorld(n)) out.set(r.id, t("pp.err.world"));
      else if (seen.has(n)) out.set(r.id, t("pp.err.dup"));
      else seen.set(n, r.id);
    }
    return out;
  }, [rows, t]);

  const dupIds = new Set([...problems].filter(([, m]) => m === t("pp.err.dup")).map(([id]) => id));
  const hardErrors = problems.size - dupIds.size;
  const finalCidrs = [...new Set(rows.map((r) => norm(r.cidr)).filter((c): c is string => !!c))];
  const before = new Set(preset?.cidrs ?? []);
  const after = new Set(finalCidrs);
  const added = [...after].filter((c) => !before.has(c));
  const removed = [...before].filter((c) => !after.has(c));
  const notesChanged = rows.some((r) => {
    const n = norm(r.cidr);
    return n && before.has(n) && (preset?.notes?.[n] ?? "") !== r.note.trim();
  });
  const metaChanged = label !== (preset?.label ?? "") || desc !== (preset?.description ?? "");
  const dedupes = (preset?.cidrs.length ?? 0) !== new Set(preset?.cidrs ?? []).size;
  const dirty = isNew || dedupes || added.length > 0 || removed.length > 0 || notesChanged || metaChanged;
  const valid = hardErrors === 0 && label.trim() !== "" && (!isNew || SLUG_RE.test(name));
  const emptyInternal = preset?.name === "internal" && finalCidrs.length === 0;

  const save = useMutation({
    mutationFn: () => {
      const notes: Record<string, string> = {};
      rows.forEach((r) => {
        const n = norm(r.cidr);
        if (n && r.note.trim()) notes[n] = r.note.trim();
      });
      return api.upsertCIDRPreset(isNew ? name : preset!.name, { label, description: desc, cidrs: finalCidrs, notes });
    },
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["cidr-presets"] });
      if (res.warn_resync) setErr(t("pp.warn.resync", { e: res.warn_resync }));
      else onClose();
    },
    onError: (e) => {
      setConfirming(false);
      setErr(e instanceof ApiError ? e.message : String(e));
    },
  });

  const del = useMutation({
    mutationFn: () => api.deleteCIDRPreset(preset!.name),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cidr-presets"] });
      onClose();
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  function addPasted() {
    const have = new Set(rows.map((r) => norm(r.cidr)));
    const fresh: Row[] = [];
    for (const line of paste.split(/\n/)) {
      // "1.2.3.4  上海办公室" or "1.2.3.4,备注"
      const m = line.trim().match(/^(\S+?)[\s,，]+(.*)$/) ?? [null, line.trim(), ""];
      const cidr = (m[1] ?? "").trim();
      if (!cidr) continue;
      const n = norm(cidr);
      if (n && have.has(n)) continue;
      have.add(n);
      fresh.push({ id: rid++, cidr, note: (m[2] ?? "").trim() });
    }
    setRows((rs) => [...fresh, ...rs]);
    setPaste("");
    setShowPaste(false);
  }

  const shown = rows.filter((r) => {
    const k = filter.trim().toLowerCase();
    return !k || r.cidr.toLowerCase().includes(k) || r.note.toLowerCase().includes(k);
  });
  const update = (id: number, patch: Partial<Row>) => setRows((rs) => rs.map((r) => (r.id === id ? { ...r, ...patch } : r)));

  return (
    <div className="animate-fade-in">
      <Button variant="ghost" size="sm" onClick={onClose} className="mb-4 -ml-2.5">
        ← {t("pp.back")}
      </Button>
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[28px] font-semibold tracking-[-0.03em] text-ink-strong">
            {isNew ? t("pp.new") : preset!.label}
          </h1>
          {!isNew && (
            <p className="mt-1 text-[13px] text-ink-muted">
              {t("pp.usedby", { n: String(preset!.project_count ?? 0) })}
            </p>
          )}
        </div>
        <div className="flex gap-2">
          {!isNew && !preset!.is_system && (
            <Button
              variant="destructive-ghost"
              onClick={async () => (await confirmDialog({ title: t("pp.delete.confirm", { label: preset!.label }), danger: true })) && del.mutate()}
            >
              {t("pp.delete")}
            </Button>
          )}
          <Button variant="secondary" onClick={onClose}>{t("pp.cancel")}</Button>
          <Button
            disabled={!dirty || !valid || emptyInternal}
            onClick={() => setConfirming(true)}
          >
            {t("pp.review")}
          </Button>
        </div>
      </div>

      {err && <Alert variant="danger" className="mb-4">{err}</Alert>}
      {dupIds.size > 0 && (
        <Alert
          variant="warning"
          className="mb-4"
          action={
            <Button variant="secondary" onClick={() => setRows((rs) => rs.filter((r) => !dupIds.has(r.id)))}>
              {t("pp.dup.fix")}
            </Button>
          }
        >
          {t("pp.dup.banner", { n: String(dupIds.size) })}
        </Alert>
      )}
      {hardErrors > 0 && (
        <Alert variant="danger" className="mb-4">{t("pp.err.banner", { n: String(hardErrors) })}</Alert>
      )}
      {emptyInternal && <Alert variant="warning" className="mb-4">{t("pp.warn.emptyInternal")}</Alert>}

      {/* Basics */}
      <Card className="mb-6">
        <CardBody className="grid grid-cols-1 gap-4 p-5 md:grid-cols-2">
          {isNew && (
            <Labeled label={t("pp.f.name")} hint={t("pp.f.name.hint")} bad={name !== "" && !SLUG_RE.test(name)}>
              <Input className="font-mono" value={name} placeholder="sg-office" onChange={(e) => setName(e.target.value.toLowerCase())} />
            </Labeled>
          )}
          <Labeled label={t("pp.f.label")}>
            <Input value={label} placeholder={t("pp.f.label.ph")} onChange={(e) => setLabel(e.target.value)} />
          </Labeled>
          <div className="md:col-span-2">
            <Labeled label={t("pp.f.desc")}>
              <Input value={desc} onChange={(e) => setDesc(e.target.value)} />
            </Labeled>
          </div>
        </CardBody>
      </Card>

      {/* Entries */}
      <Card>
        <div className="flex flex-wrap items-center gap-2 border-b border-line-subtle p-3">
          <div className="text-[14px] font-semibold text-ink-strong px-2">
            {t("pp.entries")} <span className="ml-1 font-normal text-ink-muted tabular-nums">{finalCidrs.length}</span>
          </div>
          <div className="flex-1" />
          <Input className="w-56 h-8" placeholder={t("pp.search")} value={filter} onChange={(e) => setFilter(e.target.value)} />
          <Button variant="secondary" onClick={() => setShowPaste((v) => !v)}>{t("pp.paste")}</Button>
          <Button onClick={() => setRows((rs) => [{ id: rid++, cidr: "", note: "" }, ...rs])}>+ {t("pp.add")}</Button>
        </div>

        {showPaste && (
          <div className="border-b border-line-subtle bg-canvas-surface p-4">
            <p className="mb-2 text-[12.5px] text-ink-muted">{t("pp.paste.hint")}</p>
            <Textarea
              autoFocus
              rows={10}
              className="block min-h-[220px] py-2.5 font-mono text-[12.5px]"
              placeholder={"101.47.4.16 Meegle 新加坡\n10.0.0.0/8, 内网"}
              value={paste}
              onChange={(e) => setPaste(e.target.value)}
            />
            <div className="mt-2 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setShowPaste(false)}>{t("pp.cancel")}</Button>
              <Button disabled={!paste.trim()} onClick={addPasted}>{t("pp.paste.add")}</Button>
            </div>
          </div>
        )}

        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)_40px] gap-3 border-b border-line-subtle bg-canvas-surface px-4 py-2 text-[12px] font-medium text-ink-muted">
          <span>{t("pp.col.ip")}</span><span>{t("pp.col.note")}</span><span />
        </div>
        <div className="max-h-[560px] overflow-y-auto divide-y divide-line-subtle">
          {shown.map((r) => {
            const n = norm(r.cidr);
            const isNewRow = !!n && !before.has(n);
            const prob = problems.get(r.id);
            return (
              <div key={r.id} className={cn("group grid grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)_40px] items-start gap-3 px-4 py-2", isNewRow && !isNew && "bg-status-ok-bg/40")}>
                <div>
                  <Input
                    className="h-8 font-mono text-[12.5px]"
                    aria-invalid={prob ? true : undefined}
                    value={r.cidr}
                    placeholder="1.2.3.4 或 10.0.0.0/8"
                    onChange={(e) => update(r.id, { cidr: e.target.value })}
                  />
                  {prob && <p className="mt-1 text-[11.5px] text-status-fail-fg">{prob}</p>}
                </div>
                <Input className="h-8" value={r.note} placeholder={t("pp.note.ph")} onChange={(e) => update(r.id, { note: e.target.value })} />
                <Button
                  variant="destructive-ghost"
                  size="icon"
                  title={t("pp.delete")}
                  onClick={() => setRows((rs) => rs.filter((x) => x.id !== r.id))}
                  className="text-ink-faint opacity-60 group-hover:opacity-100"
                >
                  ✕
                </Button>
              </div>
            );
          })}
          {shown.length === 0 && <div className="px-4 py-10 text-center text-[13px] text-ink-faint">{filter ? t("pp.nomatch") : t("pp.empty")}</div>}
        </div>
      </Card>

      {confirming && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 animate-fade-in" onClick={() => setConfirming(false)}>
          <div className="w-full max-w-lg rounded-lg border border-line-subtle bg-canvas-base shadow-raised" onClick={(e) => e.stopPropagation()}>
            <div className="border-b border-line-subtle p-5">
              <h3 className="text-[16px] font-semibold text-ink-strong">{t("pp.confirm.title")}</h3>
              {!isNew && <p className="mt-1 text-[13px] text-ink-muted">{t("pp.confirm.impact", { n: String(preset!.project_count ?? 0) })}</p>}
            </div>
            <div className="max-h-80 overflow-y-auto p-5 font-mono text-[12.5px] space-y-1">
              {added.map((c) => <div key={c} className="text-status-ok-fg">+ {c}</div>)}
              {removed.map((c) => <div key={c} className="text-status-fail-fg">− {c}</div>)}
              {added.length + removed.length === 0 && <div className="font-sans text-ink-muted">{t("pp.confirm.nochange")}</div>}
            </div>
            <div className="flex justify-end gap-2 border-t border-line-subtle p-4">
              <Button variant="secondary" onClick={() => setConfirming(false)}>{t("pp.cancel")}</Button>
              <Button disabled={save.isPending} onClick={() => save.mutate()}>
                {save.isPending ? "…" : t("pp.confirm.save")}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Labeled({ label, hint, bad, children }: { label: string; hint?: string; bad?: boolean; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[13px] font-medium text-ink">{label}</span>
      {children}
      {hint && <span className={cn("mt-1 block text-[12px]", bad ? "text-status-fail-fg" : "text-ink-faint")}>{hint}</span>}
    </label>
  );
}
