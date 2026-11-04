import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "../lib/api";
import type { Quota } from "../lib/types";
import { Button, Card, CardBody, CardHeader, Input, confirmDialog } from "./ui";

const FIELDS: { k: keyof Quota; label: string; hint: string }[] = [
  { k: "cpu_request", label: "CPU 申领", hint: "如 100m、0.5" },
  { k: "mem_request", label: "内存申领", hint: "如 100Mi、512Mi" },
  { k: "cpu_limit", label: "CPU 上限", hint: "如 1、2" },
  { k: "mem_limit", label: "内存上限", hint: "如 1Gi、2Gi" },
];
const empty: Quota = { cpu_request: "", mem_request: "", cpu_limit: "", mem_limit: "" };
const fail = (e: unknown) => toast.error(e instanceof ApiError ? e.message : String(e));

function Fields({ value, onChange, placeholder, disabled }: { value: Quota; onChange: (q: Quota) => void; placeholder: Quota; disabled?: boolean }) {
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      {FIELDS.map((f) => (
        <label key={f.k} className="block">
          <span className="mb-1 block text-[12.5px] font-medium text-ink">{f.label}</span>
          <Input className="font-mono" disabled={disabled} value={value[f.k]} placeholder={placeholder[f.k] || f.hint}
            onChange={(e) => onChange({ ...value, [f.k]: e.target.value })} />
        </label>
      ))}
    </div>
  );
}

// Per-project override; empty = inherit the platform default.
export function ProjectQuotaPanel({ slug }: { slug: string }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["quota", slug], queryFn: () => api.projectQuota(slug) });
  const [v, setV] = useState<Quota>(empty);
  useEffect(() => { if (q.data) setV(q.data.override); }, [q.data]);
  const save = useMutation({ mutationFn: () => api.putProjectQuota(slug, v),
    onSuccess: () => { toast.success("配额已更新，项目滚动重启中"); qc.invalidateQueries({ queryKey: ["quota", slug] }); }, onError: fail });
  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between">
        <h4 className="text-[13px] font-semibold text-ink-strong">资源配额</h4>
        <span className="text-[11.5px] text-ink-faint">留空继承默认值；保存后项目重启一次</span>
      </div>
      <Fields value={v} onChange={setV} placeholder={q.data?.effective ?? empty} disabled={!q.data?.can_edit} />
      {q.data?.can_edit && (
        <div className="mt-3 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setV(empty)}>全部继承默认</Button>
          <Button disabled={save.isPending} onClick={async () => (await confirmDialog({ title: "更新资源配额？", description: "项目会滚动重启一次。" })) && save.mutate()}>保存</Button>
        </div>
      )}
    </div>
  );
}

// Platform defaults; empty = built-in.
export function QuotaDefaults() {
  const q = useQuery({ queryKey: ["quota-defaults"], queryFn: api.quotaDefaults });
  const [v, setV] = useState<Quota>(empty);
  useEffect(() => { if (q.data) setV(q.data.defaults); }, [q.data]);
  const save = useMutation({ mutationFn: () => api.putQuotaDefaults(v), onSuccess: () => { toast.success("默认配额已保存"); q.refetch(); }, onError: fail });
  return (
    <Card>
      <CardHeader title="默认容器配额" description="每个项目 pod 默认申领的资源（调度时占用的名额）和上限。项目没单独设置时使用；改动在项目下次部署或重启时生效。留空用内置值。" />
      <CardBody className="space-y-3">
        <Fields value={v} onChange={setV} placeholder={q.data?.builtin ?? empty} />
        <div className="flex justify-end"><Button disabled={save.isPending} onClick={() => save.mutate()}>保存</Button></div>
      </CardBody>
    </Card>
  );
}
