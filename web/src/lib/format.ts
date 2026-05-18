export function timeAgo(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  const sec = Math.floor((Date.now() - d.getTime()) / 1000);
  if (sec < 0) return "in the future";
  if (sec < 60) return `${sec}s ago`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
  if (sec < 86400 * 14) return `${Math.floor(sec / 86400)}d ago`;
  return d.toLocaleDateString();
}

export function shortID(id?: string | null): string {
  if (!id) return "";
  return id.length >= 8 ? id.slice(0, 8) : id;
}
