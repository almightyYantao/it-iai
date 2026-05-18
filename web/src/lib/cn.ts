// Tiny clsx-like helper. Supports strings, falsy, and object form `{ "class": cond }`.
type Part = string | number | false | null | undefined | Record<string, unknown>;

export function cn(...parts: Part[]): string {
  const out: string[] = [];
  for (const p of parts) {
    if (!p) continue;
    if (typeof p === "string" || typeof p === "number") {
      out.push(String(p));
    } else if (typeof p === "object") {
      for (const key of Object.keys(p)) {
        if (p[key]) out.push(key);
      }
    }
  }
  return out.join(" ");
}
