import { useState } from "react";
import { cn } from "../lib/cn";
import { copyText } from "../lib/clipboard";
import { CheckIcon, ClipboardIcon } from "./icons";

// CodeBlock — a monospace block with a copy-on-click affordance. Designed
// for the Skill tutorial; useful elsewhere if we want a richer code surface
// than `<code>`. Default supports multi-line. Single-line variant collapses
// the chrome.
export function CodeBlock({
  code,
  language,
  title,
  copyable = true,
  className,
}: {
  code: string;
  language?: string;
  title?: string;
  copyable?: boolean;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  async function doCopy() {
    // copyText falls back to execCommand on HTTP origins where the
    // navigator.clipboard API is unavailable.
    const ok = await copyText(code);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  }

  return (
    <div className={cn("group/code rounded-lg border border-line-subtle bg-canvas-inset overflow-hidden", className)}>
      {(title || language) && (
        <div className="flex items-center justify-between px-4 py-2 border-b border-line-subtle/70 bg-canvas-base/40">
          <div className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
            {title ?? language}
          </div>
          {copyable && (
            <button
              type="button"
              onClick={doCopy}
              className="inline-flex items-center gap-1 text-[11px] text-ink-faint hover:text-ink-DEFAULT transition-colors"
            >
              {copied ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
              {copied ? "复制成功" : ""}
            </button>
          )}
        </div>
      )}
      <pre className="px-4 py-3 font-mono text-[12.5px] leading-[1.7] text-ink-DEFAULT overflow-x-auto selection:bg-brand/30">
        <code>{code}</code>
      </pre>
      {copyable && !(title || language) && (
        <button
          type="button"
          onClick={doCopy}
          className="absolute top-2 right-2 inline-flex items-center gap-1 rounded-md border border-line bg-canvas-base px-2 py-1 text-[11px] text-ink-faint hover:text-ink-DEFAULT opacity-0 group-hover/code:opacity-100 transition"
        >
          {copied ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
        </button>
      )}
    </div>
  );
}

export function InlineCode({ children }: { children: React.ReactNode }) {
  return (
    <code className="font-mono text-[0.92em] px-1.5 py-0.5 rounded bg-canvas-raised/50 text-ink-strong border border-line-subtle/60">
      {children}
    </code>
  );
}
