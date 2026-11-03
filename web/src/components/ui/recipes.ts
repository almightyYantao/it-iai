// Shared Vercel-style class recipes. Pages use these instead of ad-hoc strings
// so buttons / inputs / tables look and behave the same everywhere.
export const btn = {
  base: "inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors disabled:bg-canvas-raised disabled:text-ink-muted disabled:border disabled:border-line disabled:cursor-not-allowed h-8 px-3 text-[13px]",
  primary: "bg-ink-strong text-canvas-base hover:bg-ink",
  secondary: "border border-line bg-canvas-base text-ink-strong hover:bg-canvas-surface",
  ghost: "text-ink-muted hover:bg-canvas-raised hover:text-ink-strong",
  danger: "bg-status-fail-dot text-white hover:opacity-90",
};
export const input =
  "w-full h-9 rounded-md border border-line bg-canvas-base px-3 text-[13px] text-ink-strong placeholder:text-ink-faint transition-shadow focus:outline-none focus:border-line-strong focus:ring-[3px] focus:ring-ink-strong/10";
export const card = "rounded-lg border border-line-subtle bg-canvas-base";
export const th = "px-4 py-2.5 text-left text-[12px] font-medium text-ink-muted";
