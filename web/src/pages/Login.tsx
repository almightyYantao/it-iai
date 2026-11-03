import { useEffect, useState } from "react";
import { useNavigate, useLocation } from "react-router-dom";

import { api, ApiError, setToken } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { ErrorState } from "../components/States";
import { BrandMark, KeycloakIcon } from "../components/icons";
import { Button, Input, Label, Tabs } from "../components/ui";

export function Login() {
  const nav = useNavigate();
  const loc = useLocation();
  const { t, locale, setLocale } = useI18n();
  const [token, setTok] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [oidc, setOidc] = useState<boolean | null>(null);

  useEffect(() => {
    // Retry: a control-plane restart briefly 502s, and hiding SSO for good
    // because the first probe landed in that window strands users.
    let alive = true;
    const probe = (n: number) =>
      fetch("/v1/auth/oidc-status")
        .then((r) => (r.ok ? r.json() : Promise.reject()))
        .then((d) => alive && setOidc(Boolean(d?.enabled)))
        .catch(() => alive && (n > 0 ? setTimeout(() => probe(n - 1), 2000) : setOidc(false)));
    probe(5);
    return () => {
      alive = false;
    };
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
        <div className="absolute -top-1 right-0">
          <Tabs
            value={locale}
            onChange={setLocale}
            className="h-7"
            options={(["zh", "en"] as const).map((lo) => ({
              value: lo,
              label: <span className="text-[11px]">{lo === "zh" ? "中" : "EN"}</span>,
            }))}
          />
        </div>

        <div className="flex items-center gap-2 mb-1">
          <BrandMark className="size-5 text-brand" />
          <div className="font-semibold tracking-tight text-ink-strong text-[15px]">{t("app.name")}</div>
          <span className="ml-1 text-[12px] text-ink-faint">deploy</span>
        </div>
        <p className="text-[13px] text-ink-muted mb-8">{t("app.tagline")}</p>

        <form onSubmit={submit} className="space-y-3">
          <div>
            <Label className="text-[12px] font-normal text-ink-muted mb-1.5 block">{t("login.field.token")}</Label>
            <Input
              type="password"
              autoComplete="off"
              required
              value={token}
              onChange={(e) => setTok(e.target.value)}
              placeholder="vbd_live_..."
              autoFocus
              className="h-10 font-mono"
            />
          </div>

          <Button
            type="submit"
            size="lg"
            disabled={busy || !token.trim()}
            className="w-full bg-brand hover:bg-brand-hover text-[13px] font-semibold"
          >
            {busy ? t("login.button.continue.busy") : t("login.button.continue")}
          </Button>

          {oidc && (
            <>
              <div className="relative my-4">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-line-subtle" />
                </div>
                <div className="relative flex justify-center">
                  <span className="px-2 bg-canvas-base text-[12px] text-ink-faint">{t("login.or")}</span>
                </div>
              </div>
              <Button
                type="button"
                variant="secondary"
                size="lg"
                onClick={loginWithKeycloak}
                className="w-full gap-2 text-[13px]"
              >
                <KeycloakIcon className="size-4 text-brand" /> {t("login.button.oidc")}
              </Button>
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
