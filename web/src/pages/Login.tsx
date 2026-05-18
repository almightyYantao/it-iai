import { useEffect, useState } from "react";
import { useNavigate, useLocation } from "react-router-dom";

import { api, ApiError, setToken } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { ErrorState } from "../components/States";
import { BrandMark, KeycloakIcon } from "../components/icons";

export function Login() {
  const nav = useNavigate();
  const loc = useLocation();
  const { t, locale, setLocale } = useI18n();
  const [token, setTok] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [oidc, setOidc] = useState<boolean | null>(null);

  useEffect(() => {
    fetch("/v1/auth/oidc-status")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => setOidc(Boolean(d?.enabled)))
      .catch(() => setOidc(false));
  }, []);

  useEffect(() => {
    const url = new URL(window.location.href);
    const passed = url.searchParams.get("token");
    if (passed) {
      setToken(passed);
      url.searchParams.delete("token");
      window.history.replaceState({}, "", url.toString());
      nav("/", { replace: true });
    }
  }, [nav]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    setToken(token.trim());
    try {
      await api.whoami();
      const from = (loc.state as { from?: string } | null)?.from ?? "/";
      nav(from, { replace: true });
    } catch (e) {
      const msg = e instanceof ApiError ? `${e.status} ${e.message}` : String(e);
      setErr(msg);
      setToken(null);
    } finally {
      setBusy(false);
    }
  }

  function loginWithKeycloak() {
    const next = (loc.state as { from?: string } | null)?.from ?? "/";
    window.location.href = `/v1/auth/oidc-login?next=${encodeURIComponent(next)}`;
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-6 relative overflow-hidden">
      <div
        aria-hidden
        className="absolute inset-0 opacity-[0.55] pointer-events-none"
        style={{
          backgroundImage:
            "radial-gradient(circle at 1px 1px, oklch(78% 0.015 250) 1px, transparent 0)",
          backgroundSize: "24px 24px",
          maskImage:
            "radial-gradient(ellipse 60% 50% at center, black 30%, transparent 80%)",
          WebkitMaskImage:
            "radial-gradient(ellipse 60% 50% at center, black 30%, transparent 80%)",
        }}
      />

      <div className="relative w-full max-w-[380px]">
        <div className="absolute -top-1 right-0 inline-flex rounded-md border border-line-subtle bg-canvas-surface p-0.5 text-[10.5px] font-medium">
          {(["zh", "en"] as const).map((lo) => (
            <button
              key={lo}
              onClick={() => setLocale(lo)}
              className={
                "px-1.5 py-0.5 rounded transition-colors " +
                (locale === lo ? "bg-canvas-raised text-ink-strong" : "text-ink-faint hover:text-ink-DEFAULT")
              }
            >
              {lo === "zh" ? "中" : "EN"}
            </button>
          ))}
        </div>

        <div className="flex items-center gap-2 mb-1">
          <BrandMark className="size-5 text-brand" />
          <div className="font-semibold tracking-tight text-ink-strong text-[15px]">{t("app.name")}</div>
          <span className="ml-1 text-[11px] uppercase tracking-[0.12em] text-ink-faint">deploy</span>
        </div>
        <p className="text-[13px] text-ink-muted mb-8">{t("app.tagline")}</p>

        <form onSubmit={submit} className="space-y-3">
          <div>
            <label className="text-[12px] text-ink-muted mb-1.5 block">{t("login.field.token")}</label>
            <input
              type="password"
              autoComplete="off"
              required
              value={token}
              onChange={(e) => setTok(e.target.value)}
              placeholder="vbd_live_..."
              autoFocus
              className="w-full h-10 rounded-md bg-canvas-surface border border-line px-3 font-mono text-[13px] placeholder-ink-faint text-ink-DEFAULT focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand-ring transition"
            />
          </div>

          <button
            type="submit"
            disabled={busy || !token.trim()}
            className="w-full h-10 rounded-md bg-brand hover:bg-brand-hover disabled:opacity-50 disabled:cursor-not-allowed text-canvas-base text-[13px] font-semibold transition"
          >
            {busy ? t("login.button.continue.busy") : t("login.button.continue")}
          </button>

          {oidc && (
            <>
              <div className="relative my-4">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-line-subtle" />
                </div>
                <div className="relative flex justify-center">
                  <span className="px-2 bg-canvas-base text-[11px] uppercase tracking-[0.12em] text-ink-faint">{t("login.or")}</span>
                </div>
              </div>
              <button
                type="button"
                onClick={loginWithKeycloak}
                className="w-full h-10 rounded-md bg-canvas-surface hover:bg-canvas-raised border border-line text-[13px] font-medium text-ink-strong transition inline-flex items-center justify-center gap-2"
              >
                <KeycloakIcon className="size-4 text-brand" /> {t("login.button.oidc")}
              </button>
            </>
          )}

          {err && <ErrorState message={err} />}
        </form>

        <p className="mt-8 text-[11.5px] text-ink-faint leading-relaxed">
          <span dangerouslySetInnerHTML={{
            __html: t("login.footer", {
              cmd: '<code class="text-ink-muted font-mono">make seed</code>',
            }),
          }} />
        </p>
      </div>
    </div>
  );
}
