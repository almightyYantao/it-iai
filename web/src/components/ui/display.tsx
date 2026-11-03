import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/cn";

export const badgeVariants = cva("inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[11.5px] font-medium", {
  variants: {
    variant: {
      default: "border border-line-subtle bg-canvas-surface text-ink-muted",
      solid: "bg-ink-strong text-canvas-base",
      success: "bg-status-ok-bg text-status-ok-fg",
      info: "bg-status-flight-bg text-status-flight-fg",
      warning: "bg-status-warn-bg text-status-warn-fg",
      danger: "bg-status-fail-bg text-status-fail-fg",
    },
  },
  defaultVariants: { variant: "default" },
});
export function Badge({ className, variant, ...p }: React.HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ variant }), className)} {...p} />;
}

export function Card({ className, ...p }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-lg border border-line-subtle bg-canvas-base", className)} {...p} />;
}
export function CardHeader({ title, description, actions, className }: { title: React.ReactNode; description?: React.ReactNode; actions?: React.ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-start justify-between gap-3 border-b border-line-subtle px-5 py-4", className)}>
      <div className="min-w-0">
        <h3 className="text-[15px] font-semibold text-ink-strong">{title}</h3>
        {description && <p className="mt-1 text-[13px] leading-relaxed text-ink-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
export function CardBody({ className, ...p }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("px-5 py-4", className)} {...p} />;
}
export function CardFooter({ className, ...p }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center justify-end gap-2 border-t border-line-subtle bg-canvas-surface px-5 py-3", className)} {...p} />;
}

// Tables
export function Table({ className, bare, ...p }: React.TableHTMLAttributes<HTMLTableElement> & { bare?: boolean }) {
  return (
    <div className={cn("overflow-x-auto", !bare && "rounded-lg border border-line-subtle")}>
      <table className={cn("min-w-full text-[13px]", className)} {...p} />
    </div>
  );
}
export const THead = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <thead className={cn("bg-canvas-surface", p.className)} {...p} />;
export const TBody = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <tbody className={cn("divide-y divide-line-subtle", p.className)} {...p} />;
export const TR = ({ className, ...p }: React.HTMLAttributes<HTMLTableRowElement>) => <tr className={cn("transition-colors hover:bg-canvas-surface", className)} {...p} />;
export const TH = ({ className, ...p }: React.ThHTMLAttributes<HTMLTableCellElement>) => <th className={cn("h-10 px-4 text-left align-middle text-[12px] font-medium text-ink-muted", className)} {...p} />;
export const TD = ({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) => <td className={cn("px-4 py-3 align-middle text-ink", className)} {...p} />;

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-md bg-canvas-raised", className)} />;
}

export function Alert({ variant = "info", children, action, className }: { variant?: "info" | "warning" | "danger" | "success"; children: React.ReactNode; action?: React.ReactNode; className?: string }) {
  const tone = { info: "border-line-subtle bg-canvas-surface text-ink", warning: "border-status-warn-dot/30 bg-status-warn-bg text-status-warn-fg", danger: "border-status-fail-dot/30 bg-status-fail-bg text-status-fail-fg", success: "border-status-ok-dot/30 bg-status-ok-bg text-status-ok-fg" }[variant];
  return (
    <div role="alert" className={cn("flex items-center justify-between gap-3 rounded-md border px-4 py-2.5 text-[13px]", tone, className)}>
      <div className="min-w-0">{children}</div>
      {action}
    </div>
  );
}
