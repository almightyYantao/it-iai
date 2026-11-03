import * as M from "@radix-ui/react-dropdown-menu";
import { cn } from "../../lib/cn";

export const DropdownMenu = M.Root;
export const DropdownTrigger = M.Trigger;
export function DropdownContent({ className, align = "end", ...p }: M.DropdownMenuContentProps) {
  return (
    <M.Portal>
      <M.Content align={align} sideOffset={6} className={cn("z-50 min-w-[200px] rounded-lg border border-line-subtle bg-canvas-base p-1 shadow-raised animate-fade-in", className)} {...p} />
    </M.Portal>
  );
}
export function DropdownItem({ className, danger, ...p }: M.DropdownMenuItemProps & { danger?: boolean }) {
  return <M.Item className={cn("flex cursor-pointer select-none items-center gap-2 rounded-md px-2.5 py-1.5 text-[13px] outline-none data-[highlighted]:bg-canvas-raised", danger ? "text-status-fail-fg" : "text-ink data-[highlighted]:text-ink-strong", className)} {...p} />;
}
export const DropdownLabel = ({ className, ...p }: M.DropdownMenuLabelProps) => <M.Label className={cn("px-2.5 py-1.5 text-[12px] text-ink-muted", className)} {...p} />;
export const DropdownSeparator = () => <M.Separator className="my-1 h-px bg-line-subtle" />;
