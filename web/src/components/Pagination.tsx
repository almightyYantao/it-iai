import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";

// Pagination — simple offset/limit pager. Server returns `total`; we compute
// page count locally. Doesn't try to render every page button (huge total
// counts get too noisy); just prev/next + jump-to-first/last + a "{from}–{to}
// of {total}" indicator.

export type PaginationState = {
  page: number;       // 1-based
  pageSize: number;
};

export function Pagination({
  state,
  total,
  onChange,
  pageSizeOptions = [10, 25, 50, 100],
}: {
  state: PaginationState;
  total: number;
  onChange: (next: PaginationState) => void;
  pageSizeOptions?: number[];
}) {
  const { t } = useI18n();
  const { page, pageSize } = state;
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);

  const setPage = (p: number) =>
    onChange({ ...state, page: Math.max(1, Math.min(pages, p)) });
  const setPageSize = (s: number) => onChange({ page: 1, pageSize: s });

  if (total === 0) return null;

  return (
    <div className="flex items-center justify-between gap-4 mt-3 text-[12.5px] text-ink-muted">
      <div className="tabular-nums">
        {from}{from === to ? "" : "–" + to} / {total}
      </div>
      <div className="flex items-center gap-3">
        <select
          value={pageSize}
          onChange={(e) => setPageSize(Number(e.target.value))}
          className="rounded-md border border-line bg-canvas-surface px-2 py-1 text-[12px] text-ink-DEFAULT"
        >
          {pageSizeOptions.map((n) => (
            <option key={n} value={n}>{n} / {t("pagination.perpage")}</option>
          ))}
        </select>
        <div className="inline-flex rounded-md border border-line-subtle bg-canvas-surface">
          <PageBtn disabled={page <= 1} onClick={() => setPage(1)}>«</PageBtn>
          <PageBtn disabled={page <= 1} onClick={() => setPage(page - 1)}>‹</PageBtn>
          <span className="px-3 py-1 text-[12.5px] text-ink-DEFAULT tabular-nums select-none">
            {page} / {pages}
          </span>
          <PageBtn disabled={page >= pages} onClick={() => setPage(page + 1)}>›</PageBtn>
          <PageBtn disabled={page >= pages} onClick={() => setPage(pages)}>»</PageBtn>
        </div>
      </div>
    </div>
  );
}

function PageBtn({
  children,
  disabled,
  onClick,
}: {
  children: React.ReactNode;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "px-2 py-1 text-[12.5px] transition-colors first:rounded-l-md last:rounded-r-md",
        disabled
          ? "text-ink-faint cursor-not-allowed"
          : "text-ink-muted hover:text-ink-DEFAULT hover:bg-canvas-raised/60",
      )}
    >
      {children}
    </button>
  );
}
