import { fetchEventSource } from "@microsoft/fetch-event-source";
import type {
  AdminUser,
  AuditEntry,
  CIDRPreset,
  Collaborator,
  Deployment,
  DeploymentEvent,
  DomainsResponse,
  Metrics,
  PodPlacement,
  Project,
  ProjectEnvEntry,
  SystemSettings,
  SystemSettingsPatch,
  Whoami,
} from "./types";

// In dev, Vite proxies /v1 → control-plane:8080 (see vite.config.ts).
// In prod, the same origin will eventually serve both.
const BASE = "";

const TOKEN_KEY = "vibedeploy.token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string | null) {
  if (t) localStorage.setItem(TOKEN_KEY, t);
  else localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const tok = getToken();
  if (!tok) throw new ApiError(401, "no_token");
  const resp = await fetch(BASE + path, {
    method,
    headers: {
      Authorization: `Bearer ${tok}`,
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await resp.text();
  let data: any = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!resp.ok) {
    const code = data?.error?.code;
    const msg = data?.error?.message || resp.statusText || "request failed";
    throw new ApiError(resp.status, msg, code);
  }
  return data as T;
}

// All list endpoints now return { items, total, limit, offset } envelopes.
type ListResp<K extends string, T> = Record<K, T[]> & { total: number; limit: number; offset: number };

function pg(limit: number, offset: number): string {
  return `limit=${limit}&offset=${offset}`;
}

export const api = {
  whoami: () => request<Whoami>("GET", "/v1/whoami"),
  metrics: () => request<Metrics>("GET", "/v1/admin/metrics"),

  listMyProjects: (limit = 10, offset = 0) =>
    request<ListResp<"projects", Project>>("GET", `/v1/projects?${pg(limit, offset)}`),
  listAllProjects: (limit = 10, offset = 0) =>
    request<ListResp<"projects", Project>>("GET", `/v1/admin/projects?${pg(limit, offset)}`),
  getProject: (slug: string) =>
    request<{ project: Project; pod?: PodPlacement }>("GET", `/v1/projects/${encodeURIComponent(slug)}`),
  deleteProject: (slug: string) =>
    request<{ ok: boolean }>("DELETE", `/v1/projects/${encodeURIComponent(slug)}`),
  // setProjectAccess is mode-aware. Pass either:
  //   { preset: "internal" }                         → preset mode
  //   { preset: null, allowCIDRs: ["10.0.0.0/8"] }   → custom mode
  // Omitted fields stay untouched on the server.
  setProjectAccess: (
    slug: string,
    body: { preset?: string | null; allowCIDRs?: string[] },
  ) =>
    request<{ ok: boolean; preset: string | null; allow_cidrs: string[] }>(
      "PATCH",
      `/v1/projects/${encodeURIComponent(slug)}/access`,
      {
        ...(body.preset !== undefined ? { preset: body.preset } : {}),
        ...(body.allowCIDRs !== undefined ? { allow_cidrs: body.allowCIDRs } : {}),
      },
    ),

  // setProjectName updates the project's display name. The slug (URL fragment)
  // stays fixed — only the human-readable name changes.
  setProjectName: (slug: string, name: string) =>
    request<{ ok: boolean; name: string }>(
      "PATCH",
      `/v1/projects/${encodeURIComponent(slug)}/name`,
      { name },
    ),

  // setProjectTLS flips the per-project HTTPS toggle. On true, the
  // control-plane annotates the project's Ingress with the configured
  // cert-manager ClusterIssuer and cert-manager issues per-host certs.
  setProjectTLS: (slug: string, enabled: boolean) =>
    request<{ ok: boolean; tls_enabled: boolean }>(
      "PATCH",
      `/v1/projects/${encodeURIComponent(slug)}/tls`,
      { enabled },
    ),

  // regenerateProjectAPIToken mints a fresh project-level API token. The
  // plaintext comes back in the response exactly once — store it somewhere
  // safe immediately; only the hash and prefix are persisted server-side.
  regenerateProjectAPIToken: (slug: string) =>
    request<{ token: string; prefix: string }>(
      "POST",
      `/v1/projects/${encodeURIComponent(slug)}/api-token`,
      {},
    ),

  // revokeProjectAPIToken clears the token. Existing token holders 401 from
  // any token-mode path rule on the next request.
  revokeProjectAPIToken: (slug: string) =>
    request<{ ok: boolean }>(
      "DELETE",
      `/v1/projects/${encodeURIComponent(slug)}/api-token`,
    ),

  // Project path-rules CRUD. The list / create / update / delete shape mirrors
  // the backend handlers in internal/api/path_rules.go.
  listProjectPathRules: (slug: string) =>
    request<{ rules: import("./types").ProjectPathRule[] }>(
      "GET",
      `/v1/projects/${encodeURIComponent(slug)}/path-rules`,
    ),
  createProjectPathRule: (slug: string, body: { path_prefix: string; mode: "no_auth" | "token" }) =>
    request<{ rule: import("./types").ProjectPathRule }>(
      "POST",
      `/v1/projects/${encodeURIComponent(slug)}/path-rules`,
      body,
    ),
  updateProjectPathRule: (
    slug: string,
    id: string,
    body: { path_prefix: string; mode: "no_auth" | "token" },
  ) =>
    request<{ rule: import("./types").ProjectPathRule }>(
      "PATCH",
      `/v1/projects/${encodeURIComponent(slug)}/path-rules/${encodeURIComponent(id)}`,
      body,
    ),
  deleteProjectPathRule: (slug: string, id: string) =>
    request<{ ok: boolean }>(
      "DELETE",
      `/v1/projects/${encodeURIComponent(slug)}/path-rules/${encodeURIComponent(id)}`,
    ),

  // setProjectVisibility switches between the three SSO-gating modes:
  //   - "org"        any Longbridge SSO user can reach the URL.
  //   - "restricted" Longbridge SSO + project collaborator allow-list.
  //   - "public"     no SSO at all (open to anyone on the network).
  // Server re-syncs the ingress immediately so the change is live within
  // ~1s on the cluster.
  setProjectVisibility: (slug: string, visibility: "org" | "restricted" | "public") =>
    request<{ ok: boolean; visibility: string }>(
      "PATCH",
      `/v1/projects/${encodeURIComponent(slug)}/visibility`,
      { visibility },
    ),

  listCIDRPresets: () =>
    request<{ presets: CIDRPreset[] }>("GET", "/v1/admin/cidr-presets"),
  upsertCIDRPreset: (name: string, p: { label: string; description: string; cidrs: string[] }) =>
    request<{ preset: CIDRPreset; warn_resync?: string }>(
      "PUT",
      `/v1/admin/cidr-presets/${encodeURIComponent(name)}`,
      p,
    ),
  deleteCIDRPreset: (name: string) =>
    request<{ ok: boolean }>("DELETE", `/v1/admin/cidr-presets/${encodeURIComponent(name)}`),

  listDeployments: (slug: string, limit = 10, offset = 0) =>
    request<ListResp<"deployments", Deployment>>(
      "GET",
      `/v1/projects/${encodeURIComponent(slug)}/deployments?${pg(limit, offset)}`,
    ),
  getDeployment: (slug: string, id: string) =>
    request<{ deployment: Deployment }>(
      "GET",
      `/v1/projects/${encodeURIComponent(slug)}/deployments/${id}`,
    ),

  audit: (limit = 10, offset = 0) =>
    request<ListResp<"entries", AuditEntry>>("GET", `/v1/admin/audit?${pg(limit, offset)}`),

  listUsers: (limit = 10, offset = 0) =>
    request<ListResp<"users", AdminUser>>("GET", `/v1/admin/users?${pg(limit, offset)}`),
  setUserAdmin: (id: string, isAdmin: boolean) =>
    request<{ ok: boolean }>("PATCH", `/v1/admin/users/${id}`, { is_admin: isAdmin }),

  getSystemSettings: () =>
    request<SystemSettings>("GET", "/v1/admin/settings"),
  updateSystemSettings: (patch: SystemSettingsPatch) =>
    request<{ ok: boolean; changed: number }>("PATCH", "/v1/admin/settings", patch),

  listDomains: (slug: string) =>
    request<DomainsResponse>("GET", `/v1/projects/${encodeURIComponent(slug)}/domains`),
  addDomain: (slug: string, hostname: string) =>
    request<{ domain: { hostname: string } }>(
      "POST",
      `/v1/projects/${encodeURIComponent(slug)}/domains`,
      { hostname },
    ),
  removeDomain: (slug: string, hostname: string) =>
    request<{ ok: boolean }>(
      "DELETE",
      `/v1/projects/${encodeURIComponent(slug)}/domains/${encodeURIComponent(hostname)}`,
    ),

  listProjectEnv: (slug: string) =>
    request<{ env: ProjectEnvEntry[] }>("GET", `/v1/projects/${encodeURIComponent(slug)}/env`),
  setProjectEnv: (slug: string, key: string, value: string) =>
    request<{ ok: boolean; warn_resync?: string }>(
      "PUT",
      `/v1/projects/${encodeURIComponent(slug)}/env`,
      { key, value },
    ),
  deleteProjectEnv: (slug: string, key: string) =>
    request<{ ok: boolean }>(
      "DELETE",
      `/v1/projects/${encodeURIComponent(slug)}/env/${encodeURIComponent(key)}`,
    ),

  listCollaborators: (slug: string) =>
    request<{ collaborators: Collaborator[] }>("GET", `/v1/projects/${encodeURIComponent(slug)}/collaborators`),
  addCollaborator: (slug: string, email: string, role = "editor") =>
    request<{ ok: boolean }>("POST", `/v1/projects/${encodeURIComponent(slug)}/collaborators`, { email, role }),
  removeCollaborator: (slug: string, email: string) =>
    request<{ ok: boolean }>("DELETE", `/v1/projects/${encodeURIComponent(slug)}/collaborators/${encodeURIComponent(email)}`),
};

/** subscribeEvents streams deployment_events over SSE. */
export function subscribeEvents(
  slug: string,
  deploymentID: string,
  on: {
    event?: (ev: DeploymentEvent) => void;
    end?: (status: string) => void;
    error?: (err: unknown) => void;
  },
): () => void {
  const ctrl = new AbortController();
  const tok = getToken();
  fetchEventSource(`/v1/projects/${encodeURIComponent(slug)}/deployments/${deploymentID}/events`, {
    method: "GET",
    headers: tok ? { Authorization: `Bearer ${tok}` } : {},
    signal: ctrl.signal,
    openWhenHidden: true,
    onmessage(msg) {
      if (msg.event === "end") {
        try {
          const parsed = JSON.parse(msg.data || "{}");
          on.end?.(parsed.status || "");
        } catch {
          on.end?.("");
        }
        ctrl.abort();
        return;
      }
      if (msg.data) {
        try {
          const ev = JSON.parse(msg.data) as DeploymentEvent;
          on.event?.(ev);
        } catch (e) {
          on.error?.(e);
        }
      }
    },
    onerror(err) {
      on.error?.(err);
      throw err; // stop fetchEventSource from retrying — caller decides
    },
  }).catch(() => {/* aborted */});
  return () => ctrl.abort();
}
