import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { cn } from "../lib/cn";
import { copyText } from "../lib/clipboard";
import { useI18n } from "../lib/i18n";
import type { DeploymentEvent } from "../lib/types";
import { ArrowDownIcon, ClipboardIcon, CheckIcon, FilterIcon } from "./icons";

const phaseColor: Record<string, string> = {
  queue:   "text-status-flight-dot",
  upload:  "text-status-flight-dot",
  build:   "text-status-warn-dot",
  push:    "text-status-warn-dot",
  deploy:  "text-status-ok-dot",
  runtime: "text-ink-DEFAULT",
};

const PHASES = ["queue", "upload", "build", "push", "deploy", "runtime"] as const;

export function LogViewer({
  events,
  live,
}: {
  events: DeploymentEvent[];
  live: boolean;
}) {
  const { t } = useI18n();
  const [filter, setFilter] = useState<Set<string>>(new Set());
  const [atBottom, setAtBottom] = useState(true);
  const [unread, setUnread] = useState(0);
  const [copied, setCopied] = useState(false);
  const scrollerRef = useRef<HTMLDivElement>(null);

  const visible = useMemo(
    () => (filter.size === 0 ? events : events.filter((e) => filter.has(e.phase))),
    [events, filter],
  );

  useEffect(() => {
    if (atBottom) {
      requestAnimationFrame(() => {
        const el = scrollerRef.current;
        if (el) el.scrollTop = el.scrollHeight;
      });
      setUnread(0);
    } else {
      setUnread((n) => n + 1);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible.length]);

  const onScroll = useCallback(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    setAtBottom(near);
    if (near) setUnread(0);
  }, []);

  function jumpToBottom() {
    const el = scrollerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }

  async function copyAll() {
    const text = events
      .map((e) => `[${e.created_at}] [${e.phase}/${e.level}] ${e.message}`)
      .join("\n");
    // Use the helper so HTTP origins (no navigator.clipboard) still work.
    const ok = await copyText(text);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  }

  function togglePhase(p: string) {
    setFilter((cur) => {
      const next = new Set(cur);
      if (next.has(p)) next.delete(p);
      else next.add(p);
      return next;
    });
  }

  return (
    <div className="rounded-lg overflow-hidden border border-line-subtle bg-canvas-inset flex flex-col h-[70vh] relative">
      <div className="px-4 py-2.5 flex items-center gap-3 border-b border-line-subtle/70 bg-canvas-base/50">
        <FilterIcon className="size-3.5 text-ink-faint" />
        <div className="flex flex-wrap gap-1.5">
          {PHASES.map((p) => {
            const active = filter.size === 0 || filter.has(p);
            return (
              <button
                key={p}
                onClick={() => togglePhase(p)}
                className={cn(
                  "px-2 py-0.5 rounded-full text-[11px] font-medium transition-colors",
                  active ? "bg-canvas-raised text-ink-DEFAULT" : "text-ink-faint hover:text-ink-muted",
                )}
              >
                <span className={cn("inline-block size-1.5 rounded-full mr-1.5 align-middle", phaseColor[p] ? phaseColor[p].replace("text-", "bg-") : "bg-ink-faint")} />
                {p}
              </button>
            );
          })}
          {filter.size > 0 && (
            <button onClick={() => setFilter(new Set())} className="px-2 py-0.5 rounded-full text-[11px] text-ink-faint hover:text-ink-muted">
              {t("log.clear")}
            </button>
          )}
        </div>
        <div className="ml-auto flex items-center gap-3">
          <span className="text-[11.5px] text-ink-faint">
            {t("log.events", { count: visible.length })}
            {live && (
              <span className="ml-2 inline-flex items-center gap-1 text-status-flight-fg">
                <span className="relative flex size-1.5">
                  <span className="absolute inset-0 rounded-full opacity-70 animate-ping bg-status-flight-dot" />
                  <span className="relative size-1.5 rounded-full bg-status-flight-dot" />
                </span>
                {t("log.streaming")}
              </span>
            )}
          </span>
          <button onClick={copyAll} className="inline-flex items-center gap-1 text-[11.5px] text-ink-faint hover:text-ink-DEFAULT transition">
            {copied ? <CheckIcon className="size-3.5" /> : <ClipboardIcon className="size-3.5" />}
            {copied ? t("log.copied") : t("log.copy")}
          </button>
        </div>
      </div>

      <div
        ref={scrollerRef}
        onScroll={onScroll}
        className="flex-1 min-h-0 overflow-y-auto py-2 font-mono text-[12.5px] leading-[1.65] selection:bg-brand/30"
      >
        {visible.length === 0 ? (
          <div className="px-5 py-10 text-center text-ink-faint text-[12.5px]">
            {events.length === 0 ? t("log.waiting") : t("log.empty.filter")}
          </div>
        ) : (
          visible.map((e) => <LogLine key={e.id} ev={e} />)
        )}
      </div>

      {!atBottom && (
        <button
          onClick={jumpToBottom}
          className="absolute bottom-5 right-5 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-brand text-canvas-base text-[12px] font-medium shadow-lg shadow-brand/30 hover:bg-brand-hover transition"
        >
          <ArrowDownIcon className="size-3.5" />
          {unread > 0 ? t("log.new", { count: unread }) : t("log.latest")}
        </button>
      )}
    </div>
  );
}

const LogLine = memo(function LogLine({ ev }: { ev: DeploymentEvent }) {
  const ts = new Date(ev.created_at);
  const hh = String(ts.getHours()).padStart(2, "0");
  const mm = String(ts.getMinutes()).padStart(2, "0");
  const ss = String(ts.getSeconds()).padStart(2, "0");
  return (
    <div className="group grid grid-cols-[72px_84px_1fr] gap-x-3 px-4 py-[3px] hover:bg-canvas-raised/40">
      <div className="text-right text-ink-faint tabular-nums select-none">
        {hh}:{mm}:{ss}
      </div>
      <div className={cn("flex items-center gap-1.5 text-[10.5px] uppercase tracking-wider", phaseColor[ev.phase] ?? "text-ink-muted")}>
        <span className="size-1.5 rounded-full bg-current shrink-0" />
        {ev.phase}
      </div>
      <div
        className={cn("whitespace-pre-wrap break-words", {
          "text-ink-DEFAULT": ev.level === "info",
          "text-status-warn-fg": ev.level === "warn",
          "text-status-fail-fg": ev.level === "error",
        })}
      >
        {ev.message}
      </div>
    </div>
  );
});
