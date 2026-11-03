import { Button, Card, CardBody, CardHeader, Input, Table, TBody, TD, TH, THead, TR, confirmDialog } from "./ui";
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

  async function onRemove(e: string) {
    if (!(await confirmDialog({ title: t("collab.remove.confirm", { email: e }), danger: true }))) return;
    remove.mutate(e);
  }

  return (
    <Card>
      <CardHeader
        title={t("collab.heading")}
        description={t("collab.description", { owner: ownerEmail ?? t("common.empty") })}
      />

      <CardBody>
        <form onSubmit={onAdd} className="flex gap-2 mb-3">
          <Input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={t("collab.add.placeholder")}
            className="flex-1"
          />
          <Button type="submit" className="h-9" disabled={add.isPending || !email.trim()}>
            {t("collab.add.button")}
          </Button>
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
          <Table bare>
            <THead>
              <tr>
                <TH>{t("collab.col.email")}</TH>
                <TH>{t("collab.col.role")}</TH>
                <TH>{t("collab.col.added")}</TH>
                <TH className="text-right">{t("users.col.actions")}</TH>
              </tr>
            </THead>
            <TBody>
              {q.data.collaborators.map((c) => (
                <TR key={c.user_id}>
                  <TD><OwnerCell email={c.email} /></TD>
                  <TD className="text-ink-muted">
                    {c.role === "admin" ? t("collab.role.admin") : t("collab.role.editor")}
                  </TD>
                  <TD className="text-ink-muted tabular-nums">{timeAgo(c.added_at)}</TD>
                  <TD className="text-right">
                    <Button variant="destructive-ghost" size="sm" onClick={() => onRemove(c.email)}>
                      {t("common.remove")}
                    </Button>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </CardBody>
    </Card>
  );
}
