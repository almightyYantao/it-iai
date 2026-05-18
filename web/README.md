# VibeDeploy Admin (M1, WIP)

Web admin console for VibeDeploy. Built with Vite + React + TS + Tailwind.

## Status

Scaffold is in place. **Pages that are done:**
- `pages/Login.tsx` — paste a Deploy Token, validate via `/v1/whoami`
- `pages/Overview.tsx` — metric tiles (auto-refresh)
- `pages/Projects.tsx` — table of all/my projects with status + URL

**Pages still TODO** (paused mid-build):
- `pages/ProjectDetail.tsx` — deployments list, latest deployment events live
- `pages/DeploymentDetail.tsx` — full SSE event stream
- `pages/Audit.tsx` — audit log table
- `App.tsx` + `main.tsx` — top-level router wiring (uses `Layout.tsx`)

`lib/api.ts` already includes the SSE helper `subscribeEvents` (via `@microsoft/fetch-event-source`, because plain `EventSource` can't send a Bearer header).

## Dev server

```bash
cd web
npm install
npm run dev
# Vite at http://localhost:5173, proxies /v1 → control-plane :8080
```

The control-plane has been patched with a CORS middleware (`internal/api/server.go:corsMiddleware`) and three new admin endpoints:
- `GET /v1/admin/metrics`
- `GET /v1/admin/projects`
- `GET /v1/admin/audit`

All three require a token with `admin` scope (the bootstrap token has this by default).

## To resume

Implement these in order — `App.tsx` first so the existing pages can be reached:

1. `src/main.tsx` — root render with `QueryClientProvider` + `BrowserRouter`
2. `src/App.tsx` — routes: `/login`, `/`, `/projects`, `/projects/:slug`, `/projects/:slug/deployments/:id`, `/audit`, with auth guard redirecting to `/login` when no token
3. `src/pages/ProjectDetail.tsx` — project metadata + deployments table + jump to latest deployment
4. `src/pages/DeploymentDetail.tsx` — subscribe to SSE, render colored phase lines, virtualized log pane
5. `src/pages/Audit.tsx` — `useQuery(api.audit)` → table
