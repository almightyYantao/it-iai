import * as Sw from "@radix-ui/react-switch";
import * as Cb from "@radix-ui/react-checkbox";
import * as Tb from "@radix-ui/react-tabs";
import { cn } from "../../lib/cn";
import { Check } from "./select";

export function Switch({ checked, onCheckedChange, disabled, className }: { checked: boolean; onCheckedChange: (v: boolean) => void; disabled?: boolean; className?: string }) {
  return (
    <Sw.Root checked={checked} onCheckedChange={onCheckedChange} disabled={disabled}
      className={cn("relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full bg-line transition-colors data-[state=checked]:bg-ink-strong disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ink-strong/15", className)}>
      <Sw.Thumb className="block size-4 translate-x-0.5 rounded-full bg-canvas-base shadow transition-transform data-[state=checked]:translate-x-[18px]" />
    </Sw.Root>
  );
}

export function Checkbox({ checked, onCheckedChange, disabled, id }: { checked: boolean; onCheckedChange: (v: boolean) => void; disabled?: boolean; id?: string }) {
  return (
    <Cb.Root id={id} checked={checked} onCheckedChange={(v) => onCheckedChange(v === true)} disabled={disabled}
      className="flex size-4 shrink-0 items-center justify-center rounded border border-line-strong bg-canvas-base text-canvas-base data-[state=checked]:border-ink-strong data-[state=checked]:bg-ink-strong disabled:opacity-50 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ink-strong/15">
      <Cb.Indicator><Check /></Cb.Indicator>
    </Cb.Root>
  );
}

// Segmented tabs (Vercel style). Controlled; no panels — callers render content.
export function Tabs<T extends string>({ value, onChange, options, className }: { value: T; onChange: (v: T) => void; options: { value: T; label: React.ReactNode }[]; className?: string }) {
  return (
    <Tb.Root value={value} onValueChange={(v) => onChange(v as T)}>
      <Tb.List className={cn("inline-flex h-9 items-center gap-0.5 rounded-md border border-line-subtle bg-canvas-surface p-0.5", className)}>
        {options.map((o) => (
          <Tb.Trigger key={o.value} value={o.value}
            className="inline-flex h-full items-center rounded px-3 text-[13px] font-medium text-ink-muted transition-colors hover:text-ink-strong data-[state=active]:bg-canvas-base data-[state=active]:text-ink-strong data-[state=active]:shadow-soft focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ink-strong/15">
            {o.label}
          </Tb.Trigger>
        ))}
      </Tb.List>
    </Tb.Root>
  );
}
