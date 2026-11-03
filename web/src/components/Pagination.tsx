import { Button, Select } from "./ui";
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
        <Select className="h-8 w-[110px]" value={String(pageSize)} onChange={(v) => setPageSize(Number(v))}
          options={pageSizeOptions.map((n) => ({ value: String(n), label: `${n} / ${t("pagination.perpage")}` }))} />
        <div className="inline-flex h-8 items-center [&>*]:rounded-none [&>*:first-child]:rounded-l-md [&>*:last-child]:rounded-r-md [&>*+*]:-ml-px">
          <PageBtn disabled={page <= 1} onClick={() => setPage(1)}>«</PageBtn>
          <PageBtn disabled={page <= 1} onClick={() => setPage(page - 1)}>‹</PageBtn>
          <span className="flex h-8 items-center border border-line bg-canvas-base px-3 text-[13px] text-ink-strong tabular-nums select-none">
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
    <Button variant="secondary" size="icon" onClick={onClick} disabled={disabled} className="text-[14px]">
      {children}
    </Button>
  );
}
