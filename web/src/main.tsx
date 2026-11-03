import { Toaster } from "sonner";
import { ConfirmProvider } from "./components/ui/confirm";
import { TooltipProvider } from "./components/ui/tooltip";
import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { App } from "./App";
import { BrandedI18nProvider, BrandingProvider } from "./lib/branding";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 2000,
    },
  },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrandingProvider>
      <BrandedI18nProvider>
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <TooltipProvider>
              <ConfirmProvider>
                <App />
                <Toaster position="bottom-center" toastOptions={{ className: "!rounded-md !border !border-line-subtle !bg-canvas-base !text-ink-strong !text-[13px] !shadow-raised" }} />
              </ConfirmProvider>
            </TooltipProvider>
          </BrowserRouter>
        </QueryClientProvider>
      </BrandedI18nProvider>
    </BrandingProvider>
  </React.StrictMode>,
);
