import { createContext, useContext, useEffect, useState } from "react";

import { I18nProvider } from "./i18n";

// Branding — the human-readable SSO provider name shown across the UI.
//
// Loaded once at app boot from `/v1/auth/oidc-status` (an unauthenticated
// endpoint — Login page needs it before any token exists). The value lands
// in this context and is then auto-injected into every i18n t() call as
// `{{brand}}` so we don't have to thread `{ brand }` through every call site.
//
// Empty / unreachable backend → default "SSO". Operators set the real name
// in Settings → Gateway auth → "SSO brand name".

type BrandingCtxT = { brand: string };

const Ctx = createContext<BrandingCtxT>({ brand: "SSO" });

export function BrandingProvider({ children }: { children: React.ReactNode }) {
  const [brand, setBrand] = useState<string>("SSO");

  useEffect(() => {
    // fire-and-forget; if the endpoint is unreachable the default "SSO"
    // sticks. AbortController guards against state-update-after-unmount
    // warnings in dev (HMR re-mounts the provider rapidly).
    const ctrl = new AbortController();
    fetch("/v1/auth/oidc-status", { signal: ctrl.signal })
      .then((r) => (r.ok ? r.json() : null))
      .then((j: { brand?: string } | null) => {
        if (j && typeof j.brand === "string" && j.brand.trim() !== "") {
          setBrand(j.brand);
        }
      })
      .catch(() => {/* keep the default */});
    return () => ctrl.abort();
  }, []);

  return <Ctx.Provider value={{ brand }}>{children}</Ctx.Provider>;
}

export function useBranding(): BrandingCtxT {
  return useContext(Ctx);
}

// BrandedI18nProvider is the glue that hands the resolved brand to I18n —
// every t() call then sees {{brand}} auto-substituted. Must render inside
// BrandingProvider; main.tsx composes them in order.
export function BrandedI18nProvider({ children }: { children: React.ReactNode }) {
  const { brand } = useBranding();
  return <I18nProvider globalVars={{ brand }}>{children}</I18nProvider>;
}
