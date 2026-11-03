import { forwardRef } from "react";
import { cn } from "../../lib/cn";

const field =
  "w-full rounded-md border border-line bg-canvas-base px-3 text-[13px] text-ink-strong placeholder:text-ink-faint transition-[border-color,box-shadow] hover:border-line-strong focus-visible:outline-none focus-visible:border-line-strong focus-visible:ring-[3px] focus-visible:ring-ink-strong/10 disabled:cursor-not-allowed disabled:bg-canvas-surface disabled:text-ink-muted aria-[invalid=true]:border-status-fail-dot";

export const Input = forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(({ className, ...p }, ref) => (
  <input ref={ref} className={cn(field, "h-9", className)} {...p} />
));
Input.displayName = "Input";

export const Textarea = forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(({ className, ...p }, ref) => (
  <textarea ref={ref} className={cn(field, "min-h-[96px] resize-y py-2 leading-6", className)} {...p} />
));
Textarea.displayName = "Textarea";

export function Label({ className, ...p }: React.LabelHTMLAttributes<HTMLLabelElement>) {
  return <label className={cn("text-[13px] font-medium text-ink", className)} {...p} />;
}
