import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { cn } from "../lib/cn";
import { Alert, Badge, Button, Card, CardBody, CardFooter, CardHeader, Checkbox } from "./ui";

// Per-project HTTPS opt-in. Off by default; flipping it on annotates the
// Ingress with the configured cert-manager ClusterIssuer (see
// CP_TLS_CLUSTER_ISSUER on the control-plane) and adds a tls: section,
// causing cert-manager to issue per-host Let's Encrypt certs via HTTP-01.
export function ProjectTLSPanel({
  slug,
  initialEnabled,
  canEdit,
}: {
  slug: string;
  initialEnabled: boolean;
  canEdit: boolean;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();

  const [enabled, setEnabled] = useState(initialEnabled);
  const [opError, setOpError] = useState<string | null>(null);
  const [savedFlash, setSavedFlash] = useState(false);

  // Re-sync if the server view changes underneath us (other tab, project reload).
  useEffect(() => {
    setEnabled(initialEnabled);
  }, [initialEnabled]);

  const dirty = enabled !== initialEnabled;

  const save = useMutation({
    mutationFn: () => api.setProjectTLS(slug, enabled),
    onSuccess: () => {
      setOpError(null);
      setSavedFlash(true);
      setTimeout(() => setSavedFlash(false), 2400);
      qc.invalidateQueries({ queryKey: ["project", slug] });
    },
    onError: (err) => setOpError(err instanceof ApiError ? err.message : String(err)),
  });

  return (
    <Card>
      <CardHeader
        title={t("tls.title")}
        description={t("tls.desc")}
        actions={
          <Badge variant={initialEnabled ? "success" : "default"}>
            {initialEnabled ? t("tls.state.on") : t("tls.state.off")}
          </Badge>
        }
      />

      <CardBody className="space-y-3">
        <label
          className={cn(
            "flex items-start gap-3 text-[13px] text-ink-strong select-none",
            !canEdit && "opacity-60 cursor-not-allowed",
          )}
        >
          <span className="mt-0.5">
            <Checkbox checked={enabled} disabled={!canEdit || save.isPending} onCheckedChange={setEnabled} />
          </span>
          <span>{t("tls.label.enabled")}</span>
        </label>

        <p className="text-[11.5px] text-ink-muted leading-relaxed">
          {enabled ? t("tls.hint.on") : t("tls.hint.off")}
        </p>

        {opError && <Alert variant="danger">{opError}</Alert>}
      </CardBody>

      {canEdit && (
        <CardFooter className="justify-between gap-3">
          <div className="text-[11.5px] text-ink-faint">
            {savedFlash && <span className="text-status-ok-fg">{t("tls.toast.saved")}</span>}
          </div>
          <div className="flex items-center gap-2">
            {dirty && (
              <Button variant="ghost" onClick={() => setEnabled(initialEnabled)}>
                {t("access.button.reset")}
              </Button>
            )}
            <Button onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
              {save.isPending ? t("access.button.save.busy") : t("access.button.save")}
            </Button>
          </div>
        </CardFooter>
      )}
    </Card>
  );
}
