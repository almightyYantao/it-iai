import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "../lib/api";
import { Badge, Button, Input, confirmDialog } from "./ui";

type Vol = { path: string; size_gi: number };

// Admin-configured persistent paths (Longhorn). Grow only; removing a path
// unmounts it but keeps the data.
export function VolumesPanel({ slug }: { slug: string }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["volumes", slug], queryFn: () => api.listVolumes(slug) });
  const [vols, setVols] = useState<Vol[]>([]);
  const [path, setPath] = useState("");
  const [size, setSize] = useState("5");
  useEffect(() => { if (q.data) setVols(q.data.volumes); }, [q.data]);
  const save = useMutation({
    mutationFn: (next: Vol[]) => api.putVolumes(slug, next),
    onSuccess: () => { toast.success("持久化路径已更新，项目重启中"); qc.invalidateQueries({ queryKey: ["volumes", slug] }); },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : String(e)),
  });
  const edit = q.data?.can_edit;
  const status = (p: string) => q.data?.status?.find((s) => s.path === p);
  const commit = async (next: Vol[], what: string) => {
    if (await confirmDialog({ title: what, description: "挂载持久盘后项目改为「先停后启」方式更新，保存后会重启一次。" })) save.mutate(next);
  };
  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between">
        <h4 className="text-[13px] font-semibold text-ink-strong">持久化路径</h4>
        <span className="text-[11.5px] text-ink-faint">Longhorn，两台 agent 互为副本；只能扩容；移除路径会卸载但保留数据</span>
      </div>
      {q.data && !q.data.storage_ready && <p className="mb-2 text-[12.5px] text-status-warn-fg">Longhorn 还没安装，暂时不能添加持久化路径。</p>}
      <div className="rounded-md border border-line-subtle">
        {vols.length === 0 ? <p className="px-3 py-4 text-[12.5px] text-ink-faint">没有持久化路径，容器重启后写入的文件会丢失。</p> : (
          <ul className="divide-y divide-line-subtle">
            {vols.map((v, i) => (
              <li key={v.path} className="flex items-center gap-3 px-3 py-2 text-[12.5px]">
                <code className="flex-1 font-mono text-ink-strong">{v.path}</code>
                {status(v.path) && <Badge variant={status(v.path)!.phase === "Bound" ? "success" : "warning"}>{status(v.path)!.phase}{status(v.path)!.capacity ? ` · ${status(v.path)!.capacity}` : ""}</Badge>}
                {edit ? (
                  <>
                    <Input className="h-8 w-20 font-mono" type="number" min={v.size_gi} value={v.size_gi}
                      onChange={(e) => setVols(vols.map((x, j) => (j === i ? { ...x, size_gi: Number(e.target.value) } : x)))} />
                    <span className="text-ink-muted">GiB</span>
                    <Button size="sm" variant="destructive-ghost" onClick={() => commit(vols.filter((_, j) => j !== i), `卸载 ${v.path}？数据保留，可重新挂回。`)}>移除</Button>
                  </>
                ) : <span className="text-ink-muted">{v.size_gi} GiB</span>}
              </li>
            ))}
          </ul>
        )}
        {edit && (
          <div className="flex flex-wrap items-center gap-2 border-t border-line-subtle p-3">
            <Input className="min-w-[200px] flex-1 font-mono" placeholder="/app/data" value={path} onChange={(e) => setPath(e.target.value)} />
            <Input className="w-20 font-mono" type="number" min={1} max={500} value={size} onChange={(e) => setSize(e.target.value)} />
            <span className="text-[12.5px] text-ink-muted">GiB</span>
            <Button disabled={!path.startsWith("/") || !Number(size) || !q.data?.storage_ready} onClick={() => { commit([...vols, { path: path.trim(), size_gi: Number(size) }], `挂载 ${path}（${size} GiB）？`); setPath(""); }}>+ 添加</Button>
            {q.data && JSON.stringify(vols) !== JSON.stringify(q.data.volumes) && <Button variant="secondary" onClick={() => commit(vols, "保存扩容？")}>保存扩容</Button>}
          </div>
        )}
      </div>
    </div>
  );
}
