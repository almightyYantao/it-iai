export type Visibility = "org" | "restricted" | "public";

export type ProjectStatus =
  | "created"
  | "running"
  | "stopped"
  | "error"
  | "deleting";

export type DeploymentStatus =
  | "pending"
  | "queued"
  | "building"
  | "pushing"
  | "deploying"
  | "running"
  | "failed"
  | "superseded";

export interface Project {
  id: string;
  slug: string;
  name: string;
  owner_id: string;
  owner_email?: string;
  visibility: Visibility;
  status: ProjectStatus;
  url: string;
  // Empty array = no IP restriction. ONLY consulted when access_preset is
  // null/empty (custom mode). Each entry is a CIDR ("10.0.0.0/8") or
  // a bare IP (server normalises to /32 or /128 on save).
  allow_cidrs: string[];
  // Name of a row in cidr_presets. Non-empty = preset mode (the preset's
  // CIDR list wins; allow_cidrs above is ignored). Empty / null = custom mode.
  access_preset?: string | null;
  created_at: string;
  last_pushed_at?: string | null;
  last_active_at?: string | null;
}

export interface CIDRPreset {
  name: string;        // slug
  label: string;       // human-readable
  description: string;
  cidrs: string[];
  is_system: boolean;  // public/internal are system — can't be deleted
  updated_at: string;
  updated_by?: string | null;
}

// Live pod placement returned alongside the project on GET /v1/projects/{slug}.
// Omitted when no pod is scheduled or the K8s API is unreachable.
export interface PodPlacement {
  node: string;
  pod: string;
  phase: string;
  ready: boolean;
  pod_ip?: string;
  host_ip?: string;
  started?: string;
}

export interface Deployment {
  id: string;
  project_id: string;
  trigger_type: string;
  triggered_by: string;
  status: DeploymentStatus;
  source_blob_key?: string;
  image_tag?: string;
  failure_reason?: string;
  created_at: string;
  queued_at?: string;
  build_started_at?: string;
  build_done_at?: string;
  deployed_at?: string;
}

export interface DeploymentEvent {
  id: number;
  deployment_id: string;
  phase: string;
  level: "info" | "warn" | "error";
  message: string;
  created_at: string;
}

export interface Whoami {
  kind: "user" | "token";
  email?: string;
  name?: string;
  admin?: boolean;
  token_id?: string;
  scopes?: string[];
  project_id?: string | null;
}

export interface Metrics {
  projects: { total: number; running: number; failed: number };
  deployments: { last_24h: number; in_flight: number; failed_last_24h: number };
}

export interface AuditEntry {
  id: string;
  actor_type: string;
  actor_id: string;
  actor_label: string; // email (user) or token name (token); falls back to actor_id
  action: string;
  project_id?: string | null;
  metadata?: string;
  created_at: string;
}

export interface SystemSettings {
  kc: {
    issuer: string;
    jwks_url: string;
    audience: string;
    authorization_url: string;
    token_url: string;
    client_id: string;
    client_secret: string;     // "********" if a secret is configured, "" if empty
    redirect_url: string;
    auth_host: string;
    cookie_secret: string;     // same redaction sentinel as client_secret
    cookie_domain: string;
    brand_name: string;        // shown in user-facing copy via the {{brand}} placeholder
  };
  meta: Record<string, { has_override: boolean; updated_at?: string }>;
}

// Sent on PATCH /v1/admin/settings. Omit a field to leave it untouched.
// Empty string = clear the DB override and fall back to env default.
// Pass through the "********" placeholder = leave the secret unchanged.
export interface SystemSettingsPatch {
  kc: Partial<SystemSettings["kc"]>;
}

export interface AdminUser {
  id: string;
  email: string;
  name: string;
  is_admin: boolean;
  created_at: string;
  last_seen_at?: string | null;
}

export interface Collaborator {
  user_id: string;
  email: string;
  name: string;
  role: string;
  added_at: string;
}

export interface ProjectEnvEntry {
  key: string;
  updated_at: string;
  updated_by?: string | null;
  system: boolean; // true = platform-managed (e.g. auto-provisioned DATABASE_URL); UI hides Delete
}

export interface Domain {
  id: string;
  project_id: string;
  hostname: string;
  kind: string;     // "custom"
  verified: boolean;
  tls_secret?: string;
  created_at: string;
  verified_at?: string | null;
}

// Returned by GET /v1/projects/:slug/domains — splits the platform-issued
// subdomain (which the server computes, not a row in the domains table) from
// the user-added custom hostnames.
export interface DomainsResponse {
  default: { hostname: string; kind: "subdomain"; verified: boolean };
  custom: Domain[];
}

// Paged response shape — every list endpoint returns this envelope now.
export interface Paged<T> {
  total: number;
  limit: number;
  offset: number;
  items: T[];
}
