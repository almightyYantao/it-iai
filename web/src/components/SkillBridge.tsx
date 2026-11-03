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
  const [open, setOpen] = useState(false);
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
    <div className="relative">
      <button onClick={() => setOpen((v) => !v)}
        className="rounded-md border border-line-subtle px-2.5 py-1 text-[12px] font-medium text-ink-muted hover:border-line hover:text-ink-strong transition-colors">
        CLI
      </button>
      {open && (
    <div className="absolute right-0 top-9 z-50 w-80 rounded-lg border border-line-subtle bg-canvas-base px-3 py-2.5 shadow-raised animate-fade-in">
      <div className="flex items-center justify-between gap-2">
        <div className="text-[12px] text-ink-strong font-medium">
          {t("bridge.heading")}
        </div>
        <button
          type="button"
          onClick={() => setReveal((v) => !v)}
          className="text-[11px] text-ink-faint hover:text-ink transition-colors"
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
            "flex-1 inline-flex items-center justify-center gap-1.5 rounded-md border border-line bg-canvas-base hover:bg-canvas-raised px-2 py-1 text-[11.5px] text-ink transition",
          )}
          title={t("bridge.copyfull")}
        >
          {copied === "cmd" ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
          {copied === "cmd" ? t("bridge.copied") : t("bridge.copyfull")}
        </button>
        <button
          onClick={() => copy("tok")}
          className="inline-flex items-center justify-center gap-1.5 rounded-md border border-line bg-canvas-base hover:bg-canvas-raised px-2 py-1 text-[11.5px] text-ink-muted hover:text-ink transition"
          title={t("bridge.copytoken")}
        >
          {copied === "tok" ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
        </button>
      </div>
    </div>
      )}
    </div>
  );
}
