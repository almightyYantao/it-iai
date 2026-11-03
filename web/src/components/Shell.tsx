import { useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, setToken } from "../lib/api";
import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";
import { BrandMark, LogoutIcon } from "./icons";
import { SkillBridge } from "./SkillBridge";
import { ProxyDot } from "./EgressProxy";
import {
  Badge,
  Button,
  DropdownContent,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
  DropdownSeparator,
  DropdownTrigger,
  Tabs,
} from "./ui";

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
          <Badge className="text-[11px]">
            {isAdmin ? t("nav.role.admin") : t("nav.role.member")}
          </Badge>
          <div className="flex-1" />
          {isAdmin && <ProxyDot />}
          <SkillBridge />
          <ThemeToggle />
          <DropdownMenu>
            <DropdownTrigger asChild>
              <Button variant="ghost" size="icon" aria-label={email} className="rounded-full">
                <span className="flex size-7 items-center justify-center rounded-full bg-canvas-raised text-[12px] font-semibold uppercase text-ink-strong">
                  {(email || "?").slice(0, 1)}
                </span>
              </Button>
            </DropdownTrigger>
            <DropdownContent>
              <DropdownLabel>
                <div className="truncate text-[13px] font-medium text-ink-strong">{email}</div>
                <div className="mt-0.5">{isAdmin ? t("nav.role.admin") : t("nav.role.member")}</div>
              </DropdownLabel>
              <DropdownSeparator />
              <div className="flex items-center justify-between px-2.5 py-1.5">
                <span className="text-[12px] text-ink-muted">{t("nav.language")}</span>
                <LangToggle locale={locale} setLocale={setLocale} />
              </div>
              <DropdownSeparator />
              <DropdownItem danger onSelect={logout}>
                <LogoutIcon className="size-4" />
                {t("nav.signout")}
              </DropdownItem>
            </DropdownContent>
          </DropdownMenu>
        </div>
        <nav className="mx-auto flex w-full max-w-[1200px] gap-1 overflow-x-auto overflow-y-hidden px-3 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          <NavLink to="/" end className={tab}>{t("nav.overview")}</NavLink>
          <NavLink to="/projects" className={tab}>{t("nav.projects")}</NavLink>
          <NavLink to="/skill" className={tab}>{t("nav.skill")}</NavLink>
          {isAdmin && <NavLink to="/users" className={tab}>{t("nav.users")}</NavLink>}
          {isAdmin && <NavLink to="/audit" className={tab}>{t("nav.audit")}</NavLink>}
          {isAdmin && <NavLink to="/access" className={tab}>{t("nav.access")}</NavLink>}
          {isAdmin && <NavLink to="/presets" className={tab}>{t("nav.presets")}</NavLink>}
          {isAdmin && <NavLink to="/egress-logs" className={tab}>{t("nav.egresslogs")}</NavLink>}
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
    <Button variant="ghost" size="icon" onClick={flip} aria-label="theme">
      {dark ? (
        <svg className="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" strokeLinecap="round"/></svg>
      ) : (
        <svg className="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" strokeLinejoin="round"/></svg>
      )}
    </Button>
  );
}

function LangToggle({ locale, setLocale }: { locale: "zh" | "en"; setLocale: (l: "zh" | "en") => void }) {
  return (
    <Tabs
      value={locale}
      onChange={setLocale}
      className="h-7"
      options={[
        { value: "zh", label: <span aria-label="中文" className="text-[11px]">中</span> },
        { value: "en", label: <span aria-label="English" className="text-[11px]">EN</span> },
      ]}
    />
  );
}
