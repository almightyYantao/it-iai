import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "../lib/api";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";
import { PageContainer, PageHeader } from "../components/Page";
import { Alert, Badge, Button, Card, CardBody, CardHeader, Input, Switch, Table, TBody, TD, TH, THead, TR, Tabs, confirmDialog } from "../components/ui";

const gb = (b: number) => (b / 1024 ** 3).toFixed(1) + "G";
const err = (e: unknown) => toast.error(e instanceof ApiError ? e.message : String(e));

function Bar({ pct }: { pct: number }) {
  const c = pct >= 95 ? "bg-status-fail-dot" : pct >= 85 ? "bg-status-warn-dot" : "bg-status-ok-dot";
  return <div className="h-1.5 w-full overflow-hidden rounded-full bg-canvas-raised"><div className={cn("h-full", c)} style={{ width: `${Math.min(100, pct)}%` }} /></div>;
}

function Spark({ pts }: { pts: number[] }) {
  if (pts.length < 2) return <div className="h-8" />;
  const d = pts.map((v, i) => `${(i / (pts.length - 1)) * 100},${30 - (v / 100) * 30}`).join(" ");
  return <svg viewBox="0 0 100 30" preserveAspectRatio="none" className="h-8 w-full text-ink-muted"><polyline points={d} fill="none" stroke="currentColor" strokeWidth="1.2" vectorEffect="non-scaling-stroke" /></svg>;
}

export function Ops() {
  const [tab, setTab] = useState<"overview" | "agents" | "alerts">("overview");
  return (
    <PageContainer>
      <PageHeader title="运维监控" description="节点、平台服务和项目容器的实时状态，每 10 秒刷新。" actions={
        <Tabs value={tab} onChange={setTab} options={[{ value: "overview", label: "概况" }, { value: "agents", label: "Agent 管理" }, { value: "alerts", label: "告警通知" }]} />} />
      {tab === "overview" && <Overview />}
      {tab === "agents" && <Agents />}
      {tab === "alerts" && <AlertSettings />}
    </PageContainer>
  );
}

function Overview() {
  const q = useQuery({ queryKey: ["ops"], queryFn: api.ops, refetchInterval: 10000 });
  const h = useQuery({ queryKey: ["ops-history"], queryFn: () => api.opsHistory(1), refetchInterval: 60000 });
  const [search, setSearch] = useState("");
  if (q.isLoading) return <p className="text-ink-muted">加载中…</p>;
  if (q.error) return <Alert variant="danger">{String((q.error as Error).message)}</Alert>;
  const d = q.data;
  const pods = [...d.pods].filter((p: any) => !search || p.namespace.includes(search) || p.name.includes(search))
    .sort((a: any, b: any) => Number(a.ready) - Number(b.ready) || b.restarts - a.restarts);
  return (
    <div className="space-y-6">
      {d.alerts.length === 0 ? <Alert variant="success">一切正常</Alert> : (
        <div className="space-y-2">{d.alerts.map((a: any, i: number) => (
          <Alert key={i} variant={a.level === "crit" ? "danger" : "warning"} action={a.link && <Link className="underline" to={a.link}>查看</Link>}>
            <b>{a.target}</b> · {a.text}
          </Alert>))}</div>)}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {d.nodes.map((n: any) => {
          const cpu = n.cpu_cap_milli ? (n.cpu_milli * 100) / n.cpu_cap_milli : 0;
          const mem = n.mem_cap_bytes ? (n.mem_bytes * 100) / n.mem_cap_bytes : 0;
          const disk = n.disk_cap_bytes ? 100 - (n.disk_avail_bytes * 100) / n.disk_cap_bytes : 0;
          const ser = h.data?.series[n.name] ?? [];
          return (
            <Card key={n.name}><CardBody className="space-y-3">
              <div className="flex items-start justify-between gap-2">
                <div><div className="font-semibold text-ink-strong">{n.name}</div><div className="font-mono text-[12px] text-ink-faint">{n.ip} · {n.role}</div></div>
                <div className="flex gap-1">{n.cordoned && <Badge variant="warning">不可调度</Badge>}<Badge variant={n.ready ? "success" : "danger"}>{n.ready ? "Ready" : "NotReady"}</Badge></div>
              </div>
              {n.pressure.map((p: string) => <Badge key={p} variant="danger">{p}</Badge>)}
              {[["CPU", cpu, `${(n.cpu_milli / 1000).toFixed(2)} / ${n.cpu_cap_milli / 1000} 核`], ["内存", mem, `${gb(n.mem_bytes)} / ${gb(n.mem_cap_bytes)}`], ["磁盘", disk, `剩余 ${gb(n.disk_avail_bytes)}`]].map(([l, p, t]) => (
                <div key={l as string}><div className="mb-1 flex justify-between text-[12px] text-ink-muted"><span>{l} {(p as number).toFixed(0)}%</span><span>{t}</span></div><Bar pct={p as number} /></div>))}
              <div className="text-[11.5px] text-ink-faint">内存 1 小时趋势</div><Spark pts={ser.map((x) => x.mem)} />
              <div className="flex justify-between text-[12px] text-ink-muted"><span>{n.pods} 个 Pod</span><span>启动于 {timeAgo(n.started_at)}</span></div>
            </CardBody></Card>);
        })}
      </div>
      <Card><CardHeader title="平台服务" /><CardBody className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {d.services.map((s: any) => (
          <div key={s.name} className="rounded-md border border-line-subtle p-3" title={s.detail}>
            <div className="flex items-center gap-2"><span className={cn("size-2 rounded-full", s.ok ? "bg-status-ok-dot" : "bg-status-fail-dot animate-pulse")} /><span className="font-medium text-ink-strong">{s.name}</span></div>
            <div className="mt-1 truncate text-[12px] text-ink-muted">{s.detail || (s.latency_ms ? `${s.latency_ms}ms` : "正常")}</div>
          </div>))}
      </CardBody></Card>
      <Card><CardHeader title={`容器 ${d.pods.length}`} actions={<Input className="w-56" placeholder="搜索命名空间或 Pod" value={search} onChange={(e) => setSearch(e.target.value)} />} />
        <Table bare><THead><tr><TH>命名空间 / Pod</TH><TH>节点</TH><TH>状态</TH><TH>重启</TH><TH>CPU</TH><TH>内存</TH></tr></THead>
          <TBody>{pods.slice(0, 200).map((p: any) => (
            <TR key={p.namespace + p.name}>
              <TD>{p.namespace.startsWith("proj-") ? <Link className="hover:underline" to={`/projects/${p.namespace.slice(5)}`}>{p.namespace}</Link> : p.namespace}<div className="font-mono text-[11.5px] text-ink-faint">{p.name}</div></TD>
              <TD className="text-ink-muted">{p.node}</TD>
              <TD><Badge variant={p.ready ? "success" : "danger"}>{p.reason || p.phase}</Badge></TD>
              <TD className={cn("tabular-nums", p.restarts > 0 && "text-status-warn-fg")}>{p.restarts}</TD>
              <TD className="tabular-nums">{p.cpu_milli}m</TD>
              <TD className="tabular-nums">{(p.mem_bytes / 1024 ** 2).toFixed(0)}Mi</TD>
            </TR>))}</TBody></Table></Card>
    </div>
  );
}

function Agents() {
  const qc = useQueryClient();
  const ops = useQuery({ queryKey: ["ops"], queryFn: api.ops, refetchInterval: 10000 });
  const j = useQuery({ queryKey: ["agent-joins"], queryFn: api.agentJoins, refetchInterval: 5000 });
  const [name, setName] = useState("");
  const [cmd, setCmd] = useState<string | null>(null);
  const create = useMutation({ mutationFn: () => api.createAgentJoin(name), onSuccess: (r) => { setCmd(r.command); qc.invalidateQueries({ queryKey: ["agent-joins"] }); }, onError: err });
  const act = useMutation({ mutationFn: (v: { n: string; a: string }) => api.nodeAction(v.n, v.a),
    onSuccess: () => { toast.success("已执行"); qc.invalidateQueries({ queryKey: ["ops"] }); }, onError: err });
  const run = async (n: string, a: string, label: string) => (await confirmDialog({ title: `${label} ${n}？`, danger: a !== "uncordon" })) && act.mutate({ n, a });
  const stateBadge: Record<string, any> = { pending: ["default", "等待执行"], expired: ["default", "已过期"], installing: ["info", "安装中"], joined: ["warning", "已加入，未就绪"], ready: ["success", "就绪"] };
  return (
    <div className="space-y-6">
      <Card><CardHeader title="添加 Agent" description="生成一次性安装命令（30 分钟内有效，只能用一次），在新机器上用 root 执行。平台不保存任何 SSH 凭据。" />
        <CardBody className="space-y-3">
          {j.data && !j.data.configured && <Alert variant="warning">未配置 CP_K3S_SERVER_URL / CP_K3S_JOIN_TOKEN，暂不能生成命令。</Alert>}
          <div className="flex gap-2"><Input className="w-72 font-mono" placeholder="it-iai-k3s-agent-03" value={name} onChange={(e) => setName(e.target.value.toLowerCase())} />
            <Button disabled={!name || create.isPending || !j.data?.configured} onClick={() => create.mutate()}>生成安装命令</Button></div>
          {cmd && <div className="flex items-center gap-2 rounded-md bg-canvas-inset p-3 font-mono text-[12.5px]"><code className="flex-1 break-all">{cmd}</code>
            <Button size="sm" variant="secondary" onClick={() => { navigator.clipboard.writeText(cmd); toast.success("已复制"); }}>复制</Button></div>}
          {(j.data?.joins ?? []).length > 0 && <Table><THead><tr><TH>节点</TH><TH>状态</TH><TH>创建</TH><TH>执行来源</TH></tr></THead><TBody>
            {j.data!.joins.map((x: any) => <TR key={x.id}><TD className="font-mono">{x.node_name}</TD><TD><Badge variant={stateBadge[x.state][0]}>{stateBadge[x.state][1]}</Badge></TD>
              <TD className="text-ink-muted">{timeAgo(x.created_at)} · {x.created_by}</TD><TD className="font-mono text-ink-muted">{x.used_from || "—"}</TD></TR>)}</TBody></Table>}
        </CardBody></Card>
      <Card><CardHeader title="节点" />
        <Table bare><THead><tr><TH>节点</TH><TH>角色</TH><TH>状态</TH><TH>Pod</TH><TH className="text-right">操作</TH></tr></THead><TBody>
          {(ops.data?.nodes ?? []).map((n: any) => <TR key={n.name}><TD><div className="font-medium">{n.name}</div><div className="font-mono text-[11.5px] text-ink-faint">{n.ip} · {n.kubelet}</div></TD>
            <TD>{n.role}</TD><TD className="space-x-1"><Badge variant={n.ready ? "success" : "danger"}>{n.ready ? "Ready" : "NotReady"}</Badge>{n.cordoned && <Badge variant="warning">不可调度</Badge>}</TD><TD>{n.pods}</TD>
            <TD className="text-right">{n.role === "control-plane" ? <span className="text-[12px] text-ink-faint">在主机上管理</span> : <div className="flex justify-end gap-1">
              {n.cordoned ? <Button size="sm" variant="secondary" onClick={() => run(n.name, "uncordon", "恢复调度")}>恢复调度</Button>
                : <Button size="sm" variant="secondary" onClick={() => run(n.name, "cordon", "停止调度")}>停止调度</Button>}
              <Button size="sm" variant="secondary" onClick={() => run(n.name, "drain", "驱逐所有 Pod 并停止调度")}>驱逐</Button>
              <Button size="sm" variant="destructive-ghost" onClick={() => run(n.name, "delete", "从集群移除")}>移除</Button></div>}</TD></TR>)}
        </TBody></Table></Card>
    </div>
  );
}

function AlertSettings() {
  const q = useQuery({ queryKey: ["alert-settings"], queryFn: api.alertSettings });
  const [hook, setHook] = useState("");
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [dash, setDash] = useState<string | null>(null);
  const en = enabled ?? q.data?.enabled ?? false;
  const ds = dash ?? q.data?.dashboard_url ?? `${location.origin}/ops`;
  const save = useMutation({ mutationFn: () => api.putAlertSettings({ enabled: en, dashboard_url: ds, ...(hook ? { webhook_url: hook } : {}) }),
    onSuccess: () => { toast.success("已保存"); setHook(""); q.refetch(); }, onError: err });
  const test = useMutation({ mutationFn: api.testAlert, onSuccess: () => toast.success("测试消息已发送"), onError: err });
  return (
    <Card><CardHeader title="Slack 告警" description="新告警立即推送，未恢复的每 30 分钟提醒一次，恢复时推送绿色消息。消息使用 attachments 格式。" />
      <CardBody className="max-w-xl space-y-4">
        <label className="flex items-center gap-3"><Switch checked={en} onCheckedChange={setEnabled} /><span>启用推送</span></label>
        <div><div className="mb-1.5 text-[13px] font-medium">Incoming Webhook URL {q.data?.configured && <Badge variant="success">已配置</Badge>}</div>
          <Input type="password" className="font-mono" placeholder={q.data?.configured ? "已保存，留空则不修改" : "https://hooks.slack.com/services/..."} value={hook} onChange={(e) => setHook(e.target.value)} /></div>
        <div><div className="mb-1.5 text-[13px] font-medium">看板地址（消息里的跳转链接）</div><Input value={ds} onChange={(e) => setDash(e.target.value)} /></div>
        <div className="flex gap-2"><Button disabled={save.isPending} onClick={() => save.mutate()}>保存</Button>
          <Button variant="secondary" disabled={!q.data?.configured || test.isPending} onClick={() => test.mutate()}>发送测试消息</Button></div>
      </CardBody></Card>
  );
}
