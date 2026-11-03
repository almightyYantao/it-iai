import { useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, setToken } from "../lib/api";
import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";
import { BrandMark, LogoutIcon } from "./icons";
import { SkillBridge } from "./SkillBridge";

export function Shell() {
  const nav = useNavigate();
  const { t, locale, setLocale } = useI18n();
  const who = useQuery({ queryKey: ["whoami"], queryFn: api.whoami, retry: false });

  function logout() {
    setToken(null);
    nav("/login", { replace: true });
  }

  // Admin-only links: actor is admin user, or token has admin/* scope.
  const isAdmin =
    (who.data?.kind === "user" && Boolean(who.data.admin)) ||
    (who.data?.kind === "token" &&
      (who.data.scopes ?? []).some((s) => s === "admin" || s === "*"));

  const tab = ({ isActive }: { isActive: boolean }) =>
    cn(
      "relative px-3 py-3 text-[13.5px] transition-colors",
      isActive
        ? "text-ink-strong after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:bg-ink-strong"
        : "text-ink-muted hover:text-ink-strong",
    );
  const email = who.data?.kind === "user" ? who.data.email : t("nav.role.token");

  return (
    <div className="min-h-screen flex flex-col">
      <header className="sticky top-0 z-40 border-b border-line-subtle bg-canvas-base/80 backdrop-blur">
        <div className="mx-auto flex h-14 w-full max-w-[1200px] items-center gap-3 px-6">
          <BrandMark className="size-5 text-ink-strong" />
          <span className="font-semibold tracking-tight text-ink-strong">爱 AI</span>
          <span className="text-line-strong">/</span>
          <span className="rounded-full border border-line-subtle px-2 py-0.5 text-[11px] text-ink-muted">
            {isAdmin ? t("nav.role.admin") : t("nav.role.member")}
          </span>
          <div className="flex-1" />
          <SkillBridge />
          <ThemeToggle />
          <LangToggle locale={locale} setLocale={setLocale} />
          <span className="hidden sm:block max-w-[180px] truncate text-[12.5px] text-ink-muted">{email}</span>
          <button onClick={logout} title={t("nav.signout")}
            className="rounded-md p-1.5 text-ink-muted hover:bg-canvas-raised hover:text-ink-strong transition-colors">
            <LogoutIcon className="size-4" />
          </button>
        </div>
        <nav className="mx-auto flex w-full max-w-[1200px] gap-1 overflow-x-auto overflow-y-hidden px-3 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          <NavLink to="/" end className={tab}>{t("nav.overview")}</NavLink>
          <NavLink to="/projects" className={tab}>{t("nav.projects")}</NavLink>
          <NavLink to="/skill" className={tab}>{t("nav.skill")}</NavLink>
          {isAdmin && <NavLink to="/users" className={tab}>{t("nav.users")}</NavLink>}
          {isAdmin && <NavLink to="/audit" className={tab}>{t("nav.audit")}</NavLink>}
          {isAdmin && <NavLink to="/settings" className={tab}>{t("nav.settings")}</NavLink>}
        </nav>
      </header>
      <main className="flex-1 min-w-0 mx-auto w-full max-w-[1200px] animate-fade-in">
        <Outlet />
      </main>
    </div>
  );
}

function ThemeToggle() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));
  function flip() {
    const next = !dark;
    document.documentElement.classList.toggle("dark", next);
    localStorage.setItem("iai.theme", next ? "dark" : "light");
    setDark(next);
  }
  return (
    <button onClick={flip} aria-label="theme"
      className="rounded-md p-1.5 text-ink-muted hover:bg-canvas-raised hover:text-ink-strong transition-colors">
      {dark ? (
        <svg className="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" strokeLinecap="round"/></svg>
      ) : (
        <svg className="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" strokeLinejoin="round"/></svg>
      )}
    </button>
  );
}

function LangToggle({ locale, setLocale }: { locale: "zh" | "en"; setLocale: (l: "zh" | "en") => void }) {
  return (
    <div className="inline-flex rounded-md border border-line-subtle bg-canvas-surface p-0.5 text-[10.5px] font-medium">
      <button
        onClick={() => setLocale("zh")}
        className={cn(
          "px-1.5 py-0.5 rounded transition-colors",
          locale === "zh" ? "bg-canvas-raised text-ink-strong" : "text-ink-faint hover:text-ink",
        )}
        aria-label="中文"
      >
        中
      </button>
      <button
        onClick={() => setLocale("en")}
        className={cn(
          "px-1.5 py-0.5 rounded transition-colors",
          locale === "en" ? "bg-canvas-raised text-ink-strong" : "text-ink-faint hover:text-ink",
        )}
        aria-label="English"
      >
        EN
      </button>
    </div>
  );
}
