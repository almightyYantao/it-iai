import { forwardRef } from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/cn";

export const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ink-strong/15 disabled:pointer-events-none disabled:border disabled:border-line disabled:bg-canvas-raised disabled:text-ink-muted [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-ink-strong text-canvas-base hover:bg-ink",
        secondary: "border border-line bg-canvas-base text-ink-strong hover:bg-canvas-surface",
        ghost: "text-ink-muted hover:bg-canvas-raised hover:text-ink-strong",
        destructive: "bg-status-fail-dot text-white hover:opacity-90",
        "destructive-ghost": "text-ink-muted hover:bg-status-fail-bg hover:text-status-fail-fg",
        link: "h-auto px-0 text-ink-strong underline-offset-4 hover:underline",
      },
      size: { default: "h-8 px-3 text-[13px]", sm: "h-7 px-2.5 text-[12px]", lg: "h-10 px-4 text-[14px]", icon: "size-8" },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(({ className, variant, size, asChild, ...props }, ref) => {
  const C = asChild ? Slot : "button";
  return <C ref={ref} className={cn(buttonVariants({ variant, size }), className)} {...props} />;
});
Button.displayName = "Button";
