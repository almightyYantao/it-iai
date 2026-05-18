import { Link } from "react-router-dom";
import { cn } from "../lib/cn";
import { ChevronRight } from "./icons";

// PageContainer used to cap content at 1080px, which left a wide empty band on
// large displays. The sidebar (~240px) already gives layout balance — let the
// main column flex with the viewport and only keep a comfortable side gutter.
export function PageContainer({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn("w-full px-10 pt-10 pb-16", className)}>{children}</div>;
}

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
}) {
  return (
    <header className="flex items-start justify-between gap-8 mb-8">
      <div className="min-w-0">
        {eyebrow && (
          <div className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium mb-2">
            {eyebrow}
          </div>
        )}
        <h1 className="text-[26px] leading-tight font-semibold tracking-tight text-ink-strong">{title}</h1>
        {description && <p className="mt-2 text-[13.5px] text-ink-muted max-w-prose">{description}</p>}
      </div>
      {actions && <div className="shrink-0 flex items-center gap-2">{actions}</div>}
    </header>
  );
}

export function Breadcrumbs({ items }: { items: Array<{ label: string; to?: string; current?: boolean }> }) {
  return (
    <nav aria-label="breadcrumb" className="flex items-center gap-1.5 text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
      {items.map((it, i) => (
        <span key={i} className="flex items-center gap-1.5">
          {i > 0 && <ChevronRight className="size-3 opacity-60" />}
          {it.to && !it.current ? (
            <Link to={it.to} className="hover:text-ink-DEFAULT transition-colors">
              {it.label}
            </Link>
          ) : (
            <span className={cn(it.current ? "text-ink-DEFAULT normal-case tracking-normal" : "")}>{it.label}</span>
          )}
        </span>
      ))}
    </nav>
  );
}

export function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: string }[];
}) {
  return (
    <div className="inline-flex rounded-md border border-line-subtle bg-canvas-surface p-0.5 text-[12.5px]">
      {options.map((o) => (
        <button
          key={o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "px-2.5 py-1 rounded font-medium transition-colors",
            value === o.value
              ? "bg-canvas-raised text-ink-strong"
              : "text-ink-muted hover:text-ink-DEFAULT",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
