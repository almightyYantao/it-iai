export function timeAgo(iso?: string | null): string {
  if (!iso) return "—";
  const zh = (localStorage.getItem("iai.locale") ?? "zh") === "zh";
  const d = new Date(iso);
  const sec = Math.floor((Date.now() - d.getTime()) / 1000);
  const f = (n: number, zu: string, eu: string) => (zh ? `${n} ${zu}前` : `${n}${eu} ago`);
  if (sec < 0) return zh ? "刚刚" : "just now";
  if (sec < 60) return zh ? "刚刚" : `${sec}s ago`;
  if (sec < 3600) return f(Math.floor(sec / 60), "分钟", "m");
  if (sec < 86400) return f(Math.floor(sec / 3600), "小时", "h");
  if (sec < 86400 * 14) return f(Math.floor(sec / 86400), "天", "d");
  return d.toLocaleDateString(zh ? "zh-CN" : "en-US");
}

export function shortID(id?: string | null): string {
  if (!id) return "";
  return id.length >= 8 ? id.slice(0, 8) : id;
}
