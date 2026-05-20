import { useState } from "react";
import { Link } from "react-router-dom";

import { useI18n } from "../lib/i18n";
import { PageContainer, PageHeader } from "../components/Page";
import { CodeBlock, InlineCode } from "../components/CodeBlock";
import { cn } from "../lib/cn";
import { copyText } from "../lib/clipboard";
import { getToken } from "../lib/api";
import { CheckIcon, ClipboardIcon, SparklesIcon, TerminalIcon } from "../components/icons";

// Skill tutorial — quickstart that surfaces inside the admin console so a
// teammate can self-serve installing and using the Skill without leaving
// the app.
//
// The page is "live" in two places:
//   - apiBase pulled from window.location so the example URL matches whatever
//     this user is hitting (works the same in dev :5173 and prod :5173)
//   - the actual deploy token from localStorage is rendered in the Step 2
//     code block so 'copy' actually gives the user a working snippet,
//     not a placeholder they have to fill in.

// Standalone Skill repo — independent of the platform repo so business users
// pull only the ~20KB they need, not the whole control-plane / web / deploy code.
const REPO_HTTPS = "https://github.com/almightyYantao/it-iai-skill.git";
const REPO_SSH = "git@github.com:almightyYantao/it-iai-skill.git";
const REPO_DIR = "~/iai-skill";

type InstallTab = "symlink" | "copy" | "npx";

export function Skill() {
  const { t } = useI18n();
  const [tab, setTab] = useState<InstallTab>("symlink");
  const [revealToken, setRevealToken] = useState(false);
  const [copied, setCopied] = useState<"shell" | "verify" | null>(null);

  const apiBase = `${window.location.protocol}//${window.location.host}`;
  const token = getToken() ?? "";
  const tokenMasked = token ? token.slice(0, 12) + "…" : "vbd_live_…";

  // Live shell snippet — gets the real token baked in so copy → paste → done.
  const shellSnippet = `# ~/.zshrc or ~/.bashrc
export VIBEDEPLOY_API=${apiBase}
export VIBEDEPLOY_TOKEN=${revealToken ? token : tokenMasked}`;

  // What we actually copy: always the real token, regardless of mask state.
  const shellSnippetReal = `# ~/.zshrc or ~/.bashrc
export VIBEDEPLOY_API=${apiBase}
export VIBEDEPLOY_TOKEN=${token}`;

  const verifyCmd = `curl -fsS -H "Authorization: Bearer $VIBEDEPLOY_TOKEN" $VIBEDEPLOY_API/v1/whoami | jq`;

  const cloneSnippet = `git clone ${REPO_SSH} ${REPO_DIR}
# or HTTPS if you don't have SSH set up:
# git clone ${REPO_HTTPS} ${REPO_DIR}
cd ${REPO_DIR}`;

  const installSnippet: Record<InstallTab, string> = {
    symlink: `mkdir -p ~/.claude/skills
ln -s ${REPO_DIR} ~/.claude/skills/iai`,
    copy: `mkdir -p ~/.claude/skills
cp -r ${REPO_DIR} ~/.claude/skills/iai`,
    npx: `npx skills install ${REPO_DIR}`,
  };

  const tabMeta: { value: InstallTab; title: string; desc: string; tag?: string }[] = [
    {
      value: "symlink",
      title: t("skill.step1.symlink.title"),
      desc: t("skill.step1.symlink.desc"),
      tag: t("skill.step1.symlink.tag"),
    },
    { value: "copy", title: t("skill.step1.copy.title"), desc: t("skill.step1.copy.desc") },
    { value: "npx", title: t("skill.step1.npx.title"), desc: t("skill.step1.npx.desc") },
  ];

  async function copy(which: "shell" | "verify") {
    const text = which === "shell" ? shellSnippetReal : verifyCmd;
    // Use the helper so HTTP origins (no navigator.clipboard) still work.
    const ok = await copyText(text);
    if (ok) {
      setCopied(which);
      setTimeout(() => setCopied(null), 1500);
    }
  }

  return (
    <PageContainer>
      <PageHeader
        eyebrow={t("skill.eyebrow")}
        title={t("skill.title")}
        description={t("skill.description")}
      />

      {/* Quickstart: one-liner installer. Surfaced above the long-form tutorial
          because most users just want the happy path — the manual steps remain
          underneath for anyone who needs to understand what the installer does. */}
      <section className="mb-10 rounded-lg border border-brand/30 bg-brand/5 px-6 py-5">
        <div className="flex items-center gap-2 text-[11px] uppercase tracking-[0.12em] text-brand font-semibold mb-2">
          <SparklesIcon className="size-3.5" />
          {t("skill.quickstart.label")}
        </div>
        <h2 className="text-[16px] font-semibold text-ink-strong mb-1">{t("skill.quickstart.title")}</h2>
        <p className="text-[12.5px] text-ink-muted mb-3 leading-relaxed">{t("skill.quickstart.desc")}</p>
        <CodeBlock
          title="bash"
          code={`rm -rf ${REPO_DIR} && git clone ${REPO_HTTPS} ${REPO_DIR} && bash ${REPO_DIR}/install.sh install`}
        />
        <p className="text-[11.5px] text-ink-muted mt-3 leading-relaxed">
          {t("skill.quickstart.hint")}
        </p>
      </section>

      {/* Prereqs */}
      <section className="mb-12 rounded-lg border border-line-subtle bg-canvas-surface px-6 py-5">
        <div className="flex items-center gap-2 text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium mb-3">
          <SparklesIcon className="size-3.5" />
          {t("skill.prereq.title")}
        </div>
        <ul className="space-y-2 text-[13.5px] text-ink-DEFAULT">
          <PrereqItem>{t("skill.prereq.claude")}</PrereqItem>
          <PrereqItem>{t("skill.prereq.deps")}</PrereqItem>
          <PrereqItem>{t("skill.prereq.repo")}</PrereqItem>
        </ul>
      </section>

      {/* Step 1: Clone + Install */}
      <Step label={t("skill.step1.label")} title={t("skill.step1.title")}>
        <p className="text-[13.5px] text-ink-muted leading-relaxed mb-5">
          {t("skill.step1.intro")}
        </p>

        {/* 1a — clone */}
        <div className="mb-6">
          <h3 className="text-[13.5px] font-medium text-ink-strong mb-1">{t("skill.step1.clone.title")}</h3>
          <p className="text-[12.5px] text-ink-muted mb-2">{t("skill.step1.clone.desc")}</p>
          <CodeBlock title="bash" code={cloneSnippet} />
        </div>

        {/* 1b — link */}
        <div>
          <h3 className="text-[13.5px] font-medium text-ink-strong mb-1">{t("skill.step1.link.title")}</h3>
          <p className="text-[12.5px] text-ink-muted mb-3">{t("skill.step1.link.intro")}</p>
          <div className="rounded-lg border border-line-subtle bg-canvas-surface overflow-hidden mb-4">
            <div className="flex border-b border-line-subtle">
              {tabMeta.map((m) => (
                <button
                  key={m.value}
                  onClick={() => setTab(m.value)}
                  className={cn(
                    "flex-1 px-5 py-3 text-left text-[13px] transition-colors border-r border-line-subtle last:border-r-0",
                    tab === m.value ? "bg-canvas-base text-ink-strong" : "text-ink-muted hover:text-ink-DEFAULT",
                  )}
                >
                  <div className="flex items-center gap-2 mb-0.5">
                    <span className="font-medium">{m.title}</span>
                    {m.tag && (
                      <span className="rounded-full bg-status-flight-bg text-status-flight-fg px-1.5 py-0.5 text-[10px] font-medium">
                        {m.tag}
                      </span>
                    )}
                  </div>
                  <div className="text-[12px] text-ink-faint leading-snug">{m.desc}</div>
                </button>
              ))}
            </div>
            <div className="p-4">
              <CodeBlock code={installSnippet[tab]} title="bash" />
            </div>
          </div>

          <div className="mt-5">
            <div className="text-[12px] uppercase tracking-[0.12em] text-ink-faint font-medium mb-2">
              {t("skill.step1.verify.title")}
            </div>
            <p className="text-[12.5px] text-ink-muted mb-2">{t("skill.step1.verify.desc")}</p>
            <CodeBlock code={`ls ~/.claude/skills/iai/`} title="bash" />
          </div>
        </div>
      </Step>

      {/* Step 2: Token — live, copyable */}
      <Step label={t("skill.step2.label")} title={t("skill.step2.title")}>
        <p className="text-[13.5px] text-ink-muted leading-relaxed mb-4">{t("skill.step2.intro")}</p>

        {/* Live shell block with show/hide + copy */}
        <div className="rounded-lg border border-line-subtle bg-canvas-inset overflow-hidden">
          <div className="flex items-center justify-between px-4 py-2 border-b border-line-subtle/70 bg-canvas-base/40">
            <div className="text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
              ~/.zshrc · ~/.bashrc
            </div>
            <div className="flex items-center gap-3">
              <button
                onClick={() => setRevealToken((v) => !v)}
                className="text-[11.5px] text-ink-faint hover:text-ink-DEFAULT transition-colors"
              >
                {revealToken ? t("skill.step2.hide") : t("skill.step2.reveal")}
              </button>
              <button
                onClick={() => copy("shell")}
                disabled={!token}
                className="inline-flex items-center gap-1 text-[11.5px] text-ink-faint hover:text-ink-DEFAULT transition disabled:opacity-50"
              >
                {copied === "shell" ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
                {copied === "shell" ? t("skill.step2.copied") : t("skill.step2.copy")}
              </button>
            </div>
          </div>
          <pre className="px-4 py-3 font-mono text-[12.5px] leading-[1.7] text-ink-DEFAULT overflow-x-auto selection:bg-brand/30">
            <code>{shellSnippet}</code>
          </pre>
        </div>

        <div className="mt-5">
          <div className="text-[12px] uppercase tracking-[0.12em] text-ink-faint font-medium mb-2">
            {t("skill.step2.verify.title")}
          </div>
          <p className="text-[12.5px] text-ink-muted mb-2">{t("skill.step2.verify.desc")}</p>
          <div className="rounded-lg border border-line-subtle bg-canvas-inset overflow-hidden">
            <div className="flex items-center justify-end px-4 py-2 border-b border-line-subtle/70 bg-canvas-base/40">
              <button
                onClick={() => copy("verify")}
                className="inline-flex items-center gap-1 text-[11.5px] text-ink-faint hover:text-ink-DEFAULT transition"
              >
                {copied === "verify" ? <CheckIcon className="size-3" /> : <ClipboardIcon className="size-3" />}
                {copied === "verify" ? t("skill.step2.copied") : t("skill.step2.copy")}
              </button>
            </div>
            <pre className="px-4 py-3 font-mono text-[12.5px] leading-[1.7] text-ink-DEFAULT overflow-x-auto selection:bg-brand/30">
              <code>{verifyCmd}</code>
            </pre>
          </div>
        </div>

        <p className="text-[12.5px] text-ink-muted leading-relaxed mt-5">{t("skill.step2.note")}</p>
      </Step>

      {/* Step 3: Use it */}
      <Step label={t("skill.step3.label")} title={t("skill.step3.title")}>
        <p className="text-[13.5px] text-ink-muted leading-relaxed mb-4">{t("skill.step3.intro")}</p>

        <div className="rounded-lg border border-line-subtle bg-canvas-surface p-2 space-y-1">
          {[
            t("skill.step3.example1"),
            t("skill.step3.example2"),
            t("skill.step3.example3"),
            t("skill.step3.example4"),
            t("skill.step3.example5"),
            t("skill.step3.example6"),
          ].map((line, i) => (
            <div
              key={i}
              className="flex items-start gap-3 px-3 py-2 rounded-md hover:bg-canvas-raised/40"
            >
              <TerminalIcon className="size-3.5 mt-0.5 shrink-0 text-ink-faint" />
              <span className="text-[13.5px] text-ink-DEFAULT">{line}</span>
            </div>
          ))}
        </div>
      </Step>

      {/* Command reference */}
      <section className="mb-12">
        <h2 className="text-[15px] font-semibold text-ink-strong mb-1">{t("skill.commands.title")}</h2>
        <p className="text-[13px] text-ink-muted mb-4">{t("skill.commands.intro")}</p>

        <div className="overflow-hidden rounded-lg border border-line-subtle bg-canvas-surface">
          <table className="min-w-full text-[13px]">
            <thead className="bg-canvas-base/40">
              <tr className="text-left text-[11px] uppercase tracking-[0.12em] text-ink-faint font-medium">
                <th className="px-5 py-3 font-medium w-[28%]">{t("skill.commands.col.cmd")}</th>
                <th className="px-5 py-3 font-medium">{t("skill.commands.col.desc")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-subtle/60">
              <CmdRow cmd="deploy +push" desc={t("skill.commands.row.push")} />
              <CmdRow cmd="deploy +status" desc={t("skill.commands.row.status")} />
              <CmdRow cmd="deploy +logs [-f]" desc={t("skill.commands.row.logs")} />
              <CmdRow cmd="deploy +list" desc={t("skill.commands.row.list")} />
              <CmdRow cmd="deploy +share add|remove EMAIL" desc={t("skill.commands.row.share")} />
              <CmdRow cmd="deploy +whoami" desc={t("skill.commands.row.whoami")} />
            </tbody>
          </table>
        </div>
      </section>

      {/* Power moves */}
      <section className="mb-12 space-y-6">
        <h2 className="text-[15px] font-semibold text-ink-strong">{t("skill.tips.title")}</h2>

        <div>
          <h3 className="text-[13.5px] font-medium text-ink-strong mb-1">{t("skill.tips.toml.title")}</h3>
          <p className="text-[13px] text-ink-muted mb-3">{t("skill.tips.toml.intro")}</p>
          <CodeBlock
            title=".vibedeploy.toml"
            code={`name = "my-app"
port = 8000
start = "python app.py"

# Sidecar 依赖：直接写顶层（不是 [needs] section）
postgres = true
redis = false`}
          />
        </div>

        <div>
          <h3 className="text-[13.5px] font-medium text-ink-strong mb-1">{t("skill.tips.ignore.title")}</h3>
          <p className="text-[13px] text-ink-muted">{t("skill.tips.ignore.intro")}</p>
        </div>
      </section>

      {/* FAQ */}
      <section className="mb-12">
        <h2 className="text-[15px] font-semibold text-ink-strong mb-4">{t("skill.faq.title")}</h2>
        <dl className="space-y-5">
          <Faq q={t("skill.faq.q1")} a={t("skill.faq.a1")} />
          <Faq q={t("skill.faq.q2")} a={t("skill.faq.a2")} />
          <Faq q={t("skill.faq.q3")} a={t("skill.faq.a3")} />
        </dl>
      </section>

      {/* CTA */}
      <section className="rounded-lg border border-brand/40 bg-status-flight-bg/30 px-6 py-5">
        <h2 className="text-[15px] font-semibold text-ink-strong mb-1">{t("skill.cta.title")} →</h2>
        <p className="text-[13.5px] text-ink-DEFAULT">{t("skill.cta.body")}</p>
        <div className="mt-3 flex items-center gap-3">
          <Link
            to="/projects"
            className="inline-flex items-center gap-1.5 rounded-md bg-brand hover:bg-brand-hover px-3 py-1.5 text-[12.5px] font-medium text-canvas-base transition"
          >
            {t("nav.projects")} →
          </Link>
        </div>
      </section>
    </PageContainer>
  );
}

function Step({ label, title, children }: { label: string; title: string; children: React.ReactNode }) {
  return (
    <section className="mb-12 grid grid-cols-[auto_1fr] gap-x-6">
      <div className="pt-0.5">
        <div className="rounded-full bg-canvas-raised text-[11px] uppercase tracking-[0.12em] text-ink-muted font-medium px-2.5 py-1 inline-flex items-center">
          {label}
        </div>
      </div>
      <div>
        <h2 className="text-[18px] font-semibold text-ink-strong mb-3">{title}</h2>
        {children}
      </div>
    </section>
  );
}

function PrereqItem({ children }: { children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-2.5">
      <CheckIcon className="size-4 mt-0.5 shrink-0 text-status-ok-dot" />
      <span>{children}</span>
    </li>
  );
}

function CmdRow({ cmd, desc }: { cmd: string; desc: string }) {
  return (
    <tr className="hover:bg-canvas-raised/40 transition-colors">
      <td className="px-5 py-3 align-top">
        <InlineCode>{cmd}</InlineCode>
      </td>
      <td className="px-5 py-3 text-ink-muted">{desc}</td>
    </tr>
  );
}

function Faq({ q, a }: { q: string; a: string }) {
  return (
    <div>
      <dt className="text-[13.5px] font-medium text-ink-strong mb-1.5">{q}</dt>
      <dd className="text-[13px] text-ink-muted leading-relaxed">{a}</dd>
    </div>
  );
}
