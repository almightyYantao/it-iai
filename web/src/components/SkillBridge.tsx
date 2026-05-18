import { useState } from "react";
import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";
import { CheckIcon, ClipboardIcon } from "./icons";
import { getToken } from "../lib/api";
import { copyText } from "../lib/clipboard";

export function SkillBridge() {
  const { t } = useI18n();
  const token = getToken() ?? "";
  const apiBase = `${window.location.protocol}//${window.location.host}`;
  const [reveal, setReveal] = useState(false);
  const [copied, setCopied] = useState<"cmd" | "tok" | "fail" | null>(null);

  const cmd = `export VIBEDEPLOY_API=${apiBase}\nexport VIBEDEPLOY_TOKEN=${token}`;

  async function copy(what: "cmd" | "tok") {
    const ok = await copyText(what === "cmd" ? cmd : token);
    setCopied(ok ? what : "fail");
    setTimeout(() => setCopied(null), ok ? 1500 : 2500);
  }

  if (!token) return null;
  const masked = token.slice(0, 12) + "…";

  return (
    <div className="mt-4 rounded-md border border-line-subtle bg-canvas-surface px-3 py-2.5">
      <div className="flex items-center justify-between gap-2">
        <div className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
          {t("bridge.heading")}
        </div>
        <button
          type="button"
          onClick={() => setReveal((v) => !v)}
          className="text-[11px] text-ink-faint hover:text-ink-DEFAULT transition-colors"
        >
          {reveal ? t("bridge.hide") : t("bridge.show")}
        </button>
      </div>
      <div className="mt-1.5 font-mono text-[11px] text-ink-muted truncate">
        {reveal ? token : masked}
      </div>
      <div className="mt-2 flex gap-2">
        <button
          onClick={() => copy("cmd")}
          className={cn(
            "flex-1 inline-flex items-center justify-center gap-1.5 rounded-md border border-line bg-canvas-base hover:bg-canvas-raised px-2 py-1 text-[11.5px] text-ink-DEFAULT transition",
          )}
          title={t("bridge.copyfull")}
        >
          {copied === "cmd" ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
          {copied === "cmd" ? t("bridge.copied") : t("bridge.copyfull")}
        </button>
        <button
          onClick={() => copy("tok")}
          className="inline-flex items-center justify-center gap-1.5 rounded-md border border-line bg-canvas-base hover:bg-canvas-raised px-2 py-1 text-[11.5px] text-ink-muted hover:text-ink-DEFAULT transition"
          title={t("bridge.copytoken")}
        >
          {copied === "tok" ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
        </button>
      </div>
    </div>
  );
}
