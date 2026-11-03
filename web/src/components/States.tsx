import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";
import { AlertIcon } from "./icons";
import { Alert, Button, Skeleton } from "./ui";

export function EmptyState({
  title,
  description,
  icon,
  action,
}: {
  title: string;
  description?: ReactNode;
  icon?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="rounded-lg border border-dashed border-line bg-canvas-base px-10 py-20 text-center">
      {icon && <div className="text-ink-faint flex justify-center mb-3">{icon}</div>}
      <h3 className="text-[15px] font-medium text-ink-strong">{title}</h3>
      {description && (
        <p className="mt-1.5 text-[13px] text-ink-muted max-w-sm mx-auto leading-relaxed">{description}</p>
      )}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

export function LoadingRows({ rows = 5, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <div className="rounded-lg border border-line-subtle bg-canvas-base overflow-hidden">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className={cn(
            "px-5 py-4 flex gap-4 border-b border-line-subtle/60 last:border-0",
          )}
        >
          {Array.from({ length: cols }).map((_, j) => (
            <Skeleton
              key={j}
              className={cn(
                "h-3 rounded",
                j === 0 && "w-40",
                j === 1 && "w-24 ml-2",
                j === 2 && "w-16",
                j === 3 && "ml-auto w-44",
              )}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

export function ErrorState({
  message,
  onRetry,
}: {
  message: ReactNode;
  onRetry?: () => void;
}) {
  const { t } = useI18n();
  return (
    <Alert
      variant="danger"
      action={onRetry && (
        <Button variant="destructive-ghost" size="sm" onClick={onRetry}>
          {t("common.retry")}
        </Button>
      )}
    >
      <div className="flex items-start gap-3">
        <AlertIcon className="size-4 mt-0.5 shrink-0" />
        <div className="flex-1">{message}</div>
      </div>
    </Alert>
  );
}
