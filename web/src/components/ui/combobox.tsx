import { useState } from "react";
import * as P from "@radix-ui/react-popover";
import { Command } from "cmdk";
import { cn } from "../../lib/cn";
import { Check, Chevron } from "./select";

export type ComboOption = { value: string; label: string; sub?: string };

// Searchable select. value "" means "none / all" and shows the placeholder.
export function Combobox({ value, onChange, options, placeholder, searchPlaceholder, empty, allLabel, className }: {
  value: string; onChange: (v: string) => void; options: ComboOption[]; placeholder: string;
  searchPlaceholder?: string; empty?: string; allLabel?: string; className?: string;
}) {
  const [open, setOpen] = useState(false);
  const cur = options.find((o) => o.value === value);
  const pick = (v: string) => { onChange(v); setOpen(false); };
  return (
    <P.Root open={open} onOpenChange={setOpen}>
      <P.Trigger className={cn(
        "inline-flex h-9 w-full items-center justify-between gap-2 rounded-md border border-line bg-canvas-base px-3 text-[13px]",
        "transition-colors hover:border-line-strong focus:outline-none focus:ring-[3px] focus:ring-ink-strong/10", className)}>
        <span className={cn("truncate", cur ? "text-ink-strong" : "text-ink-faint")}>{cur ? cur.label : placeholder}</span>
        <Chevron />
      </P.Trigger>
      <P.Portal>
        <P.Content align="start" sideOffset={4}
          className="z-50 w-[var(--radix-popover-trigger-width)] min-w-[260px] rounded-lg border border-line-subtle bg-canvas-base shadow-raised animate-fade-in">
          <Command filter={(v, s) => (v.toLowerCase().includes(s.toLowerCase()) ? 1 : 0)}>
            <Command.Input autoFocus placeholder={searchPlaceholder}
              className="h-10 w-full border-b border-line-subtle bg-transparent px-3 text-[13px] text-ink-strong placeholder:text-ink-faint outline-none" />
            <Command.List className="max-h-[300px] overflow-y-auto p-1">
              <Command.Empty className="px-3 py-6 text-center text-[12.5px] text-ink-faint">{empty}</Command.Empty>
              {allLabel && (
                <Item selected={value === ""} onSelect={() => pick("")} keywords={allLabel}>{allLabel}</Item>
              )}
              {options.map((o) => (
                <Item key={o.value} selected={o.value === value} onSelect={() => pick(o.value)} keywords={`${o.label} ${o.sub ?? ""} ${o.value}`}>
                  <span className="truncate">{o.label}</span>
                  {o.sub && <span className="ml-2 truncate font-mono text-[11.5px] text-ink-faint">{o.sub}</span>}
                </Item>
              ))}
            </Command.List>
          </Command>
        </P.Content>
      </P.Portal>
    </P.Root>
  );
}

function Item({ selected, onSelect, keywords, children }: { selected: boolean; onSelect: () => void; keywords: string; children: React.ReactNode }) {
  return (
    <Command.Item value={keywords} onSelect={onSelect}
      className="relative flex cursor-pointer items-center rounded-md py-1.5 pl-7 pr-3 text-[13px] text-ink outline-none data-[selected=true]:bg-canvas-raised data-[selected=true]:text-ink-strong">
      {selected && <span className="absolute left-2"><Check /></span>}
      {children}
    </Command.Item>
  );
}
