import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Vite proxies /v1 → control-plane in dev so we sidestep CORS preflights for SSE.
// Production build will be served by control-plane directly (M2).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/v1": {
        target: process.env.VIBEDEPLOY_API || "http://localhost:8080",
        changeOrigin: true,
      },
      "/healthz": {
        target: process.env.VIBEDEPLOY_API || "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
