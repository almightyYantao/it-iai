package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Visibility string

const (
	VisibilityOrg        Visibility = "org"
	VisibilityRestricted Visibility = "restricted"

	// VisibilityPublic means no auth gate at all. Withdrawn 2026-07 — the
	// platform is internal-access-only, and the API rejects it (see
	// handlePatchProjectVisibility). Kept so rows written before then still
	// deserialise and so syncIngressForProject keeps treating them as ungated
	// rather than silently changing their behaviour; not selectable anywhere.
	VisibilityPublic Visibility = "public"
)

type ProjectStatus string

const (
	ProjectCreated  ProjectStatus = "created"
	ProjectRunning  ProjectStatus = "running"
	ProjectStopped  ProjectStatus = "stopped"
	ProjectError    ProjectStatus = "error"
	ProjectDeleting ProjectStatus = "deleting"
)

type DeploymentStatus string

const (
	DeployPending    DeploymentStatus = "pending"   // waiting for source upload
	DeployQueued     DeploymentStatus = "queued"    // source uploaded, waiting for builder
	DeployBuilding   DeploymentStatus = "building"
	DeployPushing    DeploymentStatus = "pushing"
	DeployDeploying  DeploymentStatus = "deploying"
	DeployRunning    DeploymentStatus = "running"
	DeployFailed     DeploymentStatus = "failed"
	DeploySuperseded DeploymentStatus = "superseded"
)

// Manifest is the contract between Skill and Control Plane describing how to build & run a project.
type Manifest struct {
	Name     string            `json:"name"`
	Port     int               `json:"port,omitempty"`
	Build    string            `json:"build,omitempty"`
	Start    string            `json:"start,omitempty"`
	Language string            `json:"language,omitempty"`
	Needs    Needs             `json:"needs,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Resources Resources        `json:"resources,omitempty"`
}

type Needs struct {
	Postgres bool `json:"postgres,omitempty"`
	Redis    bool `json:"redis,omitempty"`
	// S3 → the platform auto-creates a private bucket on the shared MinIO and
	// hands the project an isolated access-key pair scoped to it. Injected env
	// vars: S3_ENDPOINT / S3_REGION / S3_ACCESS_KEY_ID / S3_SECRET_ACCESS_KEY /
	// S3_BUCKET / S3_USE_SSL.
	S3 bool `json:"s3,omitempty"`
	// SQLite → the pod gets an emptyDir at /data + a Litestream sidecar that
	// replicates /data/app.db to the project's S3 bucket continuously. An init
	// container restores from S3 on pod start. Implies Needs.S3 (replication
	// target). Injected env: SQLITE_PATH=/data/app.db. App must honour
	// SQLITE_PATH (or default it to /data/app.db) or its data won't be
	// replicated.
	SQLite bool `json:"sqlite,omitempty"`
}

type Resources struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type Project struct {
	ID            uuid.UUID       `json:"id"`
	Slug          string          `json:"slug"`
	Name          string          `json:"name"`
	OwnerID       uuid.UUID       `json:"owner_id"`
	Visibility    Visibility      `json:"visibility"`
	Status        ProjectStatus   `json:"status"`
	Manifest      json.RawMessage `json:"manifest,omitempty"`
	// AllowCIDRs restricts ingress traffic to these CIDR ranges. Empty means
	// "no restriction". Used as the EFFECTIVE allow-list ONLY when
	// AccessPreset is null — otherwise the preset's cidrs win. Kept so
	// projects can opt out of the standardised presets when they need a
	// one-off list (e.g. demo apps with vendor-specific access).
	AllowCIDRs    []string        `json:"allow_cidrs"`
	// AccessPreset, when non-empty, names a row in cidr_presets that supplies
	// the effective allow-list. Lets ops maintain a few canonical lists
	// ("internal", "public", ...) globally instead of per-project. Null means
	// "use this project's own AllowCIDRs (custom mode)".
	AccessPreset  *string         `json:"access_preset,omitempty"`
	// TLSEnabled requests an HTTPS-only Ingress for this project: cert-manager
	// issues a per-host Let's Encrypt cert via HTTP-01, and Traefik serves it
	// on :443. Off by default; flip via PATCH /v1/projects/:slug/tls. The
	// platform needs cert-manager + a `letsencrypt-prod` ClusterIssuer for
	// this to do anything (deploy/install-cert-manager.sh).
	TLSEnabled    bool            `json:"tls_enabled"`
	// APITokenPrefix is the first ~16 chars of the plaintext token, kept for
	// UI display so owners can recognise which token a teammate has pasted
	// somewhere. The hash itself is never exposed in JSON; the plaintext is
	// returned exactly once at regeneration time and never re-derivable.
	// Empty when the project has no API token yet.
	APITokenPrefix    string     `json:"api_token_prefix,omitempty"`
	APITokenCreatedAt *time.Time `json:"api_token_created_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	LastPushedAt  *time.Time      `json:"last_pushed_at,omitempty"`
	LastActiveAt  *time.Time      `json:"last_active_at,omitempty"`
	URL           string          `json:"url,omitempty"` // computed
}

// ProjectPathRule overrides the project's default SSO gate for requests whose
// URL path starts with PathPrefix. Longer prefixes win when multiple could
// match; the deployer emits IngressRoute routes in that order.
type ProjectPathRule struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"project_id"`
	PathPrefix string    `json:"path_prefix"`
	// Mode is one of: "no_auth" (skip ForwardAuth entirely, IP allow-list
	// still applies) or "token" (require Authorization: Bearer <project api
	// token>).
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	PathRuleModeNoAuth = "no_auth"
	PathRuleModeToken  = "token"
)

type Deployment struct {
	ID              uuid.UUID        `json:"id"`
	ProjectID       uuid.UUID        `json:"project_id"`
	TriggerType     string           `json:"trigger_type"`
	TriggeredBy     string           `json:"triggered_by"`
	Status          DeploymentStatus `json:"status"`
	SourceBlobKey   string           `json:"source_blob_key,omitempty"`
	ImageTag        string           `json:"image_tag,omitempty"`
	Manifest        json.RawMessage  `json:"manifest"`
	FailureReason   string           `json:"failure_reason,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	QueuedAt        *time.Time       `json:"queued_at,omitempty"`
	BuildStartedAt  *time.Time       `json:"build_started_at,omitempty"`
	BuildDoneAt     *time.Time       `json:"build_done_at,omitempty"`
	DeployedAt      *time.Time       `json:"deployed_at,omitempty"`
}

type DeploymentEvent struct {
	ID           int64     `json:"id"`
	DeploymentID uuid.UUID `json:"deployment_id"`
	Phase        string    `json:"phase"`
	Level        string    `json:"level"`
	Message      string    `json:"message"`
	CreatedAt    time.Time `json:"created_at"`
}

type User struct {
	ID         uuid.UUID `json:"id"`
	KCSub      string    `json:"kc_sub,omitempty"`
	Email      string    `json:"email"`
	Name       string    `json:"name,omitempty"`
	IsAdmin    bool      `json:"is_admin"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

type DeployToken struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	Scopes      []string   `json:"scopes"`
	AllowDB     bool       `json:"allow_db"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// CIDRPreset is a named, globally-maintained allow-list. Projects point at one
// by name via Project.AccessPreset instead of each owner curating their own
// CIDR list. The two system presets (`public`, `internal`) are seeded by
// migration 0005 and can't be deleted (is_system=true).
type CIDRPreset struct {
	Name        string     `json:"name"`        // slug, also the API key
	Label       string     `json:"label"`       // display name shown in the UI dropdown
	Description string     `json:"description"`
	CIDRs       []string   `json:"cidrs"`       // empty = "no restriction"
	IsSystem    bool       `json:"is_system"`   // system presets refuse DELETE
	UpdatedAt   time.Time  `json:"updated_at"`
	UpdatedBy   *uuid.UUID `json:"updated_by,omitempty"`
}
