import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api, setToken } from "../lib/api";
import { cn } from "../lib/cn";
import { useI18n } from "../lib/i18n";
import { ActivityIcon, BookIcon, BrandMark, LayersIcon, LogoutIcon, ScrollIcon, SettingsIcon, UsersIcon } from "./icons";
import { SkillBridge } from "./SkillBridge";

export function Shell() {
  const nav = useNavigate();
  const { t, locale, setLocale } = useI18n();
  const who = useQuery({ queryKey: ["whoami"], queryFn: api.whoami, retry: false });

  function logout() {
    setToken(null);
    nav("/login", { replace: true });
  }

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    cn(
      "group flex items-center gap-2.5 px-3 py-1.5 rounded-md text-[13px] transition-colors",
      isActive
        ? "bg-canvas-raised text-ink-strong"
        : "text-ink-muted hover:text-ink-DEFAULT hover:bg-canvas-surface/60",
    );

  const iconCls = "size-3.5 shrink-0 opacity-70 group-hover:opacity-100";

  // Admin-only links: actor is admin user, or token has admin/* scope.
  const isAdmin =
    (who.data?.kind === "user" && Boolean(who.data.admin)) ||
    (who.data?.kind === "token" &&
      (who.data.scopes ?? []).some((s) => s === "admin" || s === "*"));

  return (
    <div className="min-h-screen flex">
      <aside className="hidden md:flex w-60 shrink-0 flex-col border-r border-line-subtle px-3 py-5 bg-canvas-base sticky top-0 h-screen overflow-y-auto">
        <div className="px-3 mb-7 flex items-center gap-2">
          <BrandMark className="size-5 text-brand" />
          <div className="font-semibold tracking-tight text-ink-strong">爱 AI</div>
          <span className="ml-auto text-[10px] uppercase tracking-[0.12em] text-ink-faint">deploy</span>
        </div>

        <div className="px-3 mb-2 text-[10.5px] uppercase tracking-[0.12em] text-ink-faint font-medium">
          {t("nav.section")}
        </div>
        <nav className="space-y-0.5">
          <NavLink to="/" end className={linkClass}>
            <ActivityIcon className={iconCls} /> {t("nav.overview")}
          </NavLink>
          <NavLink to="/projects" className={linkClass}>
            <LayersIcon className={iconCls} /> {t("nav.projects")}
          </NavLink>
          <NavLink to="/skill" className={linkClass}>
            <BookIcon className={iconCls} /> {t("nav.skill")}
          </NavLink>
          {isAdmin && (
            <NavLink to="/users" className={linkClass}>
              <UsersIcon className={iconCls} /> {t("nav.users")}
            </NavLink>
          )}
          {isAdmin && (
            <NavLink to="/audit" className={linkClass}>
              <ScrollIcon className={iconCls} /> {t("nav.audit")}
            </NavLink>
          )}
          {isAdmin && (
            <NavLink to="/settings" className={linkClass}>
              <SettingsIcon className={iconCls} /> {t("nav.settings")}
            </NavLink>
          )}
        </nav>

        <div className="flex-1" />

        <SkillBridge />

        <div className="px-3 py-3 border-t border-line-subtle/70 mt-3">
          {who.data?.kind === "user" && (
            <>
              <div className="text-[12px] text-ink-DEFAULT truncate">{who.data.email}</div>
              <div className="text-[11px] text-ink-faint mt-0.5">
                {who.data.admin ? t("nav.role.admin") : t("nav.role.member")}
              </div>
            </>
          )}
          {who.data?.kind === "token" && (
            <>
              <div className="text-[12px] text-ink-DEFAULT">{t("nav.role.token")}</div>
              <div className="text-[11px] text-ink-faint mt-0.5 font-mono truncate">
                {(who.data.scopes ?? []).join(", ") || t("nav.role.scopes.none")}
              </div>
            </>
          )}

          <div className="mt-3 flex items-center justify-between">
            <button
              onClick={logout}
              className="inline-flex items-center gap-1.5 text-[11.5px] text-ink-faint hover:text-status-fail-fg transition-colors"
            >
              <LogoutIcon className="size-3.5" /> {t("nav.signout")}
            </button>
            <LangToggle locale={locale} setLocale={setLocale} />
          </div>
        </div>
      </aside>

      <main className="flex-1 min-w-0 min-h-screen">
        <Outlet />
      </main>
    </div>
  );
}

function LangToggle({ locale, setLocale }: { locale: "zh" | "en"; setLocale: (l: "zh" | "en") => void }) {
  return (
    <div className="inline-flex rounded-md border border-line-subtle bg-canvas-surface p-0.5 text-[10.5px] font-medium">
      <button
        onClick={() => setLocale("zh")}
        className={cn(
          "px-1.5 py-0.5 rounded transition-colors",
          locale === "zh" ? "bg-canvas-raised text-ink-strong" : "text-ink-faint hover:text-ink-DEFAULT",
        )}
        aria-label="中文"
      >
        中
      </button>
      <button
        onClick={() => setLocale("en")}
        className={cn(
          "px-1.5 py-0.5 rounded transition-colors",
          locale === "en" ? "bg-canvas-raised text-ink-strong" : "text-ink-faint hover:text-ink-DEFAULT",
        )}
        aria-label="English"
      >
        EN
      </button>
    </div>
  );
}
