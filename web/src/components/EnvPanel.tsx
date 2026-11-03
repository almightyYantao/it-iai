import { Alert, Badge, Button, Card, CardBody, CardHeader, Input, Table, TBody, TD, TH, THead, TR, confirmDialog } from "./ui";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
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

  async function onRemove(key: string) {
    if (!(await confirmDialog({ title: t("env.remove.confirm", { key }), danger: true }))) return;
    remove.mutate(key);
  }

  const rows = q.data?.env ?? [];

  return (
    <Card>
      <CardHeader title={t("env.heading")} description={t("env.description")} />

      <CardBody>
        <Table bare>
          <THead>
            <tr>
              <TH>{t("env.col.key")}</TH>
              <TH>{t("env.col.updated")}</TH>
              <TH className="text-right">{t("env.col.actions")}</TH>
            </tr>
          </THead>
          <TBody>
            {rows.map((e) => (
              <TR key={e.key}>
                <TD>
                  <span className="font-mono text-[12.5px] text-ink-strong">{e.key}</span>
                  {e.system && (
                    <Badge className="ml-2" title={t("env.system.hint")}>
                      {t("env.system.tag")}
                    </Badge>
                  )}
                </TD>
                <TD className="text-ink-muted tabular-nums">{timeAgo(e.updated_at)}</TD>
                <TD className="text-right">
                  {canEdit && !e.system && (
                    <Button
                      variant="destructive-ghost"
                      size="sm"
                      onClick={() => onRemove(e.key)}
                      disabled={remove.isPending}
                    >
                      ✕
                    </Button>
                  )}
                </TD>
              </TR>
            ))}
            {q.data && rows.length === 0 && (
              <tr>
                <TD colSpan={3} className="text-[12.5px] text-ink-faint italic">
                  {t("env.empty")}
                </TD>
              </tr>
            )}
          </TBody>
        </Table>

        {canEdit && (
          <form onSubmit={onSave} className="mt-5 space-y-2">
            <div className="flex gap-2">
              <Input
                value={keyInput}
                onChange={(e) => setKeyInput(e.target.value)}
                placeholder={t("env.add.key.placeholder")}
                spellCheck={false}
                autoComplete="off"
                className="w-44 font-mono"
              />
              <Input
                value={valueInput}
                onChange={(e) => setValueInput(e.target.value)}
                placeholder={t("env.add.value.placeholder")}
                type="password"
                spellCheck={false}
                autoComplete="new-password"
                className="flex-1 font-mono"
              />
              <Button type="submit" className="h-9" disabled={save.isPending || keyInput.trim() === ""}>
                {save.isPending ? t("env.add.busy") : t("env.add.button")}
              </Button>
            </div>
            <p className="text-[11.5px] text-ink-muted leading-relaxed">{t("env.add.hint")}</p>
          </form>
        )}

        {opError && <Alert variant="danger" className="mt-3">{opError}</Alert>}
      </CardBody>
    </Card>
  );
}
