import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { toast } from "sonner";

import { api, ApiError, getToken } from "../lib/api";
import { cn } from "../lib/cn";
import { timeAgo } from "../lib/format";
import { Badge, Button, Card, CardBody, CardHeader, Input, Select, Switch, Tabs } from "./ui";

// Runtime panel on the project page: live container logs for anyone who can
// see the project, an interactive shell for the owner and admins.

export function RuntimePanel({ slug }: { slug: string }) {
  const pods = useQuery({ queryKey: ["pods", slug], queryFn: () => api.projectPods(slug), refetchInterval: 10000 });
  const list = pods.data?.pods ?? [];
  const [pod, setPod] = useState("");
  const [container, setContainer] = useState("app");
  const [tab, setTab] = useState<"logs" | "shell">("logs");
  const cur = list.find((p) => p.name === pod) ?? list[0];
  useEffect(() => {
    if (!pod && list[0]) setPod(list[0].name);
  }, [list, pod]);
  const containers = cur?.containers.map((c) => c.name) ?? ["app"];

  return (
    <Card>
      <CardHeader
        title="容器"
        description="实时日志；负责人和管理员可以进入终端。终端会话会写入审计日志。"
        actions={<Tabs value={tab} onChange={setTab} options={[{ value: "logs", label: "日志" }, ...(pods.data?.can_exec ? [{ value: "shell" as const, label: "终端" }] : [])]} />}
      />
      <CardBody className="space-y-3">
        {list.length === 0 ? (
          <p className="text-[13px] text-ink-faint">没有运行中的容器</p>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <Select className="w-80" value={cur?.name ?? ""} onChange={setPod}
              options={list.map((p) => ({ value: p.name, label: p.name, hint: `${p.phase} · ${p.node || "—"}` }))} />
            <Select className="w-40" value={containers.includes(container) ? container : containers[0]} onChange={setContainer}
              options={containers.map((c) => ({ value: c, label: c }))} />
            {cur && cur.containers.map((c) => (
              <Badge key={c.name} variant={c.ready ? "success" : "danger"}>{c.name}: {c.state}{c.restarts ? ` · 重启 ${c.restarts}` : ""}</Badge>
            ))}
            {cur?.started_at && <span className="text-[12px] text-ink-faint">启动于 {timeAgo(cur.started_at)}</span>}
          </div>
        )}
        {cur && tab === "logs" && <LogStream key={cur.name + container} slug={slug} pod={cur.name} container={containers.includes(container) ? container : containers[0]} />}
        {cur && tab === "shell" && <Shell key={cur.name + container} slug={slug} pod={cur.name} container={containers.includes(container) ? container : containers[0]} />}
      </CardBody>
    </Card>
  );
}

function LogStream({ slug, pod, container }: { slug: string; pod: string; container: string }) {
  const [lines, setLines] = useState<string[]>([]);
  const [follow, setFollow] = useState(true);
  const [previous, setPrevious] = useState(false);
  const [filter, setFilter] = useState("");
  const [ts, setTs] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  useEffect(() => {
    setLines([]);
    const ctl = new AbortController();
    const qs = new URLSearchParams({ pod, container, tail: "1000", follow: follow && !previous ? "1" : "0", previous: previous ? "1" : "0" });
    (async () => {
      try {
        const resp = await fetch(`/v1/projects/${encodeURIComponent(slug)}/logs?${qs}`, {
          headers: { Authorization: `Bearer ${getToken() ?? ""}` }, signal: ctl.signal,
        });
        if (!resp.ok || !resp.body) {
          setLines([`读取日志失败：HTTP ${resp.status}${previous ? "（可能没有上一次运行的日志）" : ""}`]);
          return;
        }
        const reader = resp.body.getReader();
        const dec = new TextDecoder();
        let buf = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += dec.decode(value, { stream: true });
          const parts = buf.split("\n\n");
          buf = parts.pop() ?? "";
          const add: string[] = [];
          for (const p of parts) {
            if (!p.startsWith("data: ")) continue;
            try { add.push(JSON.parse(p.slice(6))); } catch { /* ignore */ }
          }
          if (add.length) setLines((l) => (l.length + add.length > 5000 ? [...l, ...add].slice(-5000) : [...l, ...add]));
        }
      } catch (e) {
        if (!ctl.signal.aborted) setLines((l) => [...l, `连接中断：${String(e)}`]);
      }
    })();
    return () => ctl.abort();
  }, [slug, pod, container, follow, previous]);

  useEffect(() => {
    if (stick.current && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [lines]);

  const shown = filter ? lines.filter((l) => l.toLowerCase().includes(filter.toLowerCase())) : lines;
  const fmt = (l: string) => (ts ? l : l.replace(/^\S+Z\s/, ""));
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-3 text-[12.5px] text-ink-muted">
        <Input className="h-8 w-56" placeholder="过滤关键字" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <label className="flex items-center gap-1.5"><Switch checked={follow} onCheckedChange={setFollow} disabled={previous} />实时跟随</label>
        <label className="flex items-center gap-1.5"><Switch checked={previous} onCheckedChange={setPrevious} />上一次运行（崩溃前）</label>
        <label className="flex items-center gap-1.5"><Switch checked={ts} onCheckedChange={setTs} />时间戳</label>
        <span className="ml-auto tabular-nums">{shown.length} 行</span>
        <Button size="sm" variant="secondary" onClick={() => {
          const blob = new Blob([lines.join("\n")], { type: "text/plain" });
          const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = `${pod}-${container}.log`; a.click();
        }}>下载</Button>
      </div>
      <div ref={box} onScroll={(e) => { const el = e.currentTarget; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40; }}
        className="h-[480px] overflow-auto rounded-md bg-[#0a0a0a] p-3 font-mono text-[12px] leading-5 text-[#e5e5e5]">
        {shown.length === 0 ? <span className="text-[#777]">暂无日志…</span> : shown.map((l, i) => (
          <div key={i} className={cn("whitespace-pre-wrap break-all", /error|exception|fatal|panic/i.test(l) && "text-[#ff6b6b]", /warn/i.test(l) && "text-[#f5a524]")}>{fmt(l)}</div>
        ))}
      </div>
    </div>
  );
}

function Shell({ slug, pod, container }: { slug: string; pod: string; container: string }) {
  const el = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<"idle" | "connecting" | "open" | "closed">("idle");
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    if (nonce === 0 || !el.current) return;
    const term = new Terminal({ fontFamily: '"Geist Mono", ui-monospace, Menlo, monospace', fontSize: 13, cursorBlink: true, theme: { background: "#0a0a0a" } });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el.current);
    fit.fit();
    let ws: WebSocket | null = null;
    setState("connecting");
    api.execTicket(slug, pod, container).then(({ ticket }) => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      ws = new WebSocket(`${proto}://${location.host}/v1/exec?ticket=${encodeURIComponent(ticket)}`);
      ws.binaryType = "arraybuffer";
      const send = (o: object) => ws?.readyState === 1 && ws.send(JSON.stringify(o));
      ws.onopen = () => { setState("open"); send({ type: "resize", cols: term.cols, rows: term.rows }); term.focus(); };
      ws.onmessage = (ev) => term.write(typeof ev.data === "string" ? ev.data : new Uint8Array(ev.data));
      ws.onclose = () => { setState("closed"); term.write("\r\n\x1b[90m[会话已结束]\x1b[0m\r\n"); };
      term.onData((d) => send({ type: "input", data: d }));
      term.onResize(({ cols, rows }) => send({ type: "resize", cols, rows }));
    }).catch((e) => { setState("closed"); toast.error(e instanceof ApiError ? e.message : String(e)); });
    const ro = new ResizeObserver(() => fit.fit());
    ro.observe(el.current);
    return () => { ro.disconnect(); ws?.close(); term.dispose(); };
  }, [nonce, slug, pod, container]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-3 text-[12.5px] text-ink-muted">
        <span>进入 <code className="font-mono text-ink-strong">{pod}/{container}</code>，会话最长 2 小时</span>
        <Button size="sm" className="ml-auto" variant={state === "open" ? "secondary" : "default"} onClick={() => setNonce((n) => n + 1)}>
          {state === "idle" ? "连接终端" : state === "connecting" ? "连接中…" : "重新连接"}
        </Button>
      </div>
      <div ref={el} className={cn("h-[480px] overflow-hidden rounded-md bg-[#0a0a0a] p-2", state === "idle" && "grid place-items-center")}>
        {state === "idle" && <span className="text-[13px] text-[#777]">点「连接终端」进入容器</span>}
      </div>
    </div>
  );
}
