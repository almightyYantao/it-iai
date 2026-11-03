import { createContext, useCallback, useContext, useRef, useState } from "react";
import * as D from "@radix-ui/react-dialog";
import { cn } from "../../lib/cn";
import { btn } from "./recipes";
import { useI18n } from "../../lib/i18n";

type Opts = { title: string; description?: React.ReactNode; confirmText?: string; danger?: boolean };
const Ctx = createContext<(o: Opts) => Promise<boolean>>(async () => false);

// useConfirm() replaces window.confirm with a styled, keyboard-friendly dialog.
export const useConfirm = () => useContext(Ctx);

// Imperative form for plain handlers: await confirmDialog({...}).
let globalAsk: ((o: Opts) => Promise<boolean>) | null = null;
export const confirmDialog = (o: Opts) => (globalAsk ? globalAsk(o) : Promise.resolve(window.confirm(o.title)));

export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const { t } = useI18n();
  const [opts, setOpts] = useState<Opts | null>(null);
  const resolver = useRef<(v: boolean) => void>();
  const ask = useCallback((o: Opts) => new Promise<boolean>((res) => { resolver.current = res; setOpts(o); }), []);
  globalAsk = ask;
  const done = (v: boolean) => { resolver.current?.(v); setOpts(null); };
  return (
    <Ctx.Provider value={ask}>
      {children}
      <D.Root open={!!opts} onOpenChange={(o) => !o && done(false)}>
        <D.Portal>
          <D.Overlay className="fixed inset-0 z-50 bg-black/40 animate-fade-in" />
          <D.Content className="fixed left-1/2 top-1/2 z-50 w-[calc(100%-32px)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-line-subtle bg-canvas-base shadow-raised animate-fade-in focus:outline-none">
            <div className="p-5">
              <D.Title className="text-[15px] font-semibold text-ink-strong">{opts?.title}</D.Title>
              {opts?.description && <D.Description className="mt-2 text-[13px] leading-relaxed text-ink-muted">{opts.description}</D.Description>}
            </div>
            <div className="flex justify-end gap-2 border-t border-line-subtle bg-canvas-surface px-5 py-3">
              <button className={cn(btn.base, btn.secondary)} onClick={() => done(false)}>{t("ui.cancel")}</button>
              <button autoFocus className={cn(btn.base, opts?.danger ? btn.danger : btn.primary)} onClick={() => done(true)}>
                {opts?.confirmText ?? t("ui.confirm")}
              </button>
            </div>
          </D.Content>
        </D.Portal>
      </D.Root>
    </Ctx.Provider>
  );
}
