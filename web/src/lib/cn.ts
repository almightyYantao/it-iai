import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

// clsx + tailwind-merge: later classes win on conflict (w-64 overrides w-full).
export function cn(...parts: ClassValue[]): string {
  return twMerge(clsx(parts));
}
