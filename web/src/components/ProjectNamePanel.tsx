import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { Alert, Button, Card, CardBody, CardHeader, Input } from "./ui";

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
    <Card>
      <CardHeader title={t("name.title")} description={t("name.desc")} />

      <CardBody className="space-y-3">
        {isDefaulted && <Alert variant="warning">{t("name.warning.defaulted", { slug })}</Alert>}

        <div className="flex items-center gap-2">
          <Input
            type="text"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            disabled={!canEdit || save.isPending}
            spellCheck={false}
            maxLength={120}
            placeholder={t("name.placeholder")}
            className="flex-1"
          />
          {canEdit && (
            <Button onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
              {save.isPending ? t("access.button.save.busy") : t("access.button.save")}
            </Button>
          )}
        </div>

        <div className="flex items-center justify-between text-[11.5px] text-ink-faint">
          <span>
            {t("name.slug.label")}: <span className="font-mono">{slug}</span> {t("name.slug.fixed")}
          </span>
          {savedFlash && <span className="text-status-ok-fg">{t("access.toast.saved")}</span>}
        </div>

        {opError && <Alert variant="danger">{opError}</Alert>}
      </CardBody>
    </Card>
  );
}
