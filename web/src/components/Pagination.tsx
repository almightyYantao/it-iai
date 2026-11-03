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
    <div className="flex items-center justify-between gap-4 mt-4 text-[13px] text-ink-muted">
      <div className="tabular-nums">
        {from}{from === to ? "" : "–" + to} / {total}
      </div>
      <div className="flex items-center gap-3">
        <select
          value={pageSize}
          onChange={(e) => setPageSize(Number(e.target.value))}
          className="h-8 rounded-md border border-line bg-canvas-base px-2 text-[13px] text-ink focus:outline-none focus:border-line-strong"
        >
          {pageSizeOptions.map((n) => (
            <option key={n} value={n}>{n} / {t("pagination.perpage")}</option>
          ))}
        </select>
        <div className="inline-flex h-8 items-center overflow-hidden rounded-md border border-line bg-canvas-base divide-x divide-line">
          <PageBtn disabled={page <= 1} onClick={() => setPage(1)}>«</PageBtn>
          <PageBtn disabled={page <= 1} onClick={() => setPage(page - 1)}>‹</PageBtn>
          <span className="px-3 text-[13px] text-ink-strong tabular-nums select-none">
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
        "flex h-full w-8 items-center justify-center text-[14px] transition-colors",
        disabled
          ? "text-ink-faint cursor-not-allowed"
          : "text-ink-muted hover:text-ink-strong hover:bg-canvas-surface",
      )}
    >
      {children}
    </button>
  );
}
