import * as S from "@radix-ui/react-select";
import { cn } from "../../lib/cn";

export type Option<T extends string> = { value: T; label: React.ReactNode; hint?: React.ReactNode };

export function Select<T extends string>({ value, onChange, options, placeholder, className, disabled }: {
  value: T; onChange: (v: T) => void; options: Option<T>[]; placeholder?: string; className?: string; disabled?: boolean;
}) {
  return (
    <S.Root value={value} onValueChange={(v) => onChange(v as T)} disabled={disabled}>
      <S.Trigger className={cn(
        "inline-flex h-9 w-full items-center justify-between gap-2 rounded-md border border-line bg-canvas-base px-3 text-[13px] text-ink-strong",
        "transition-colors hover:border-line-strong focus:outline-none focus:ring-[3px] focus:ring-ink-strong/10 data-[placeholder]:text-ink-faint disabled:opacity-50", className)}>
        <S.Value placeholder={placeholder} />
        <S.Icon><Chevron /></S.Icon>
      </S.Trigger>
      <S.Portal>
        <S.Content position="popper" sideOffset={4}
          className="z-50 max-h-[320px] min-w-[var(--radix-select-trigger-width)] overflow-hidden rounded-lg border border-line-subtle bg-canvas-base p-1 shadow-raised animate-fade-in">
          <S.Viewport>
            {options.map((o) => (
              <S.Item key={o.value} value={o.value}
                className="relative flex cursor-pointer select-none items-center rounded-md py-1.5 pl-7 pr-3 text-[13px] text-ink outline-none data-[highlighted]:bg-canvas-raised data-[highlighted]:text-ink-strong">
                <S.ItemIndicator className="absolute left-2"><Check /></S.ItemIndicator>
                <S.ItemText>{o.label}</S.ItemText>
                {o.hint && <span className="ml-auto pl-3 text-[12px] text-ink-faint">{o.hint}</span>}
              </S.Item>
            ))}
          </S.Viewport>
        </S.Content>
      </S.Portal>
    </S.Root>
  );
}

export const Chevron = () => (
  <svg className="size-3.5 text-ink-faint" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6"><path d="m4 6 4 4 4-4" strokeLinecap="round" strokeLinejoin="round" /></svg>
);
export const Check = () => (
  <svg className="size-3.5" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="m3.5 8.5 3 3 6-7" strokeLinecap="round" strokeLinejoin="round" /></svg>
);
