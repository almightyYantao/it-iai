-- Named CIDR allow-lists ("internal", "public", ...) maintained globally so
-- projects pick one from a dropdown instead of each project owner curating
-- their own. Empty cidrs[] = no restriction (= "public").
CREATE TABLE IF NOT EXISTS cidr_presets (
    name        text PRIMARY KEY,        -- slug, used as the API key
    label       text NOT NULL,           -- human-readable display name
    description text NOT NULL DEFAULT '',
    cidrs       text[] NOT NULL DEFAULT '{}',
    is_system   boolean NOT NULL DEFAULT false, -- system presets (internal/public) can't be deleted
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid REFERENCES users(id)
);

-- Project picks a preset by name. NULL means "use the project's own
-- allow_cidrs column" — the legacy per-project mode, preserved for projects
-- that need a one-off allow-list not worth standardising.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS access_preset text REFERENCES cidr_presets(name) ON DELETE SET NULL;

-- Seed the two standard presets.
--   - public: empty cidrs[] → no IP restriction (still goes through SSO
--     unless visibility=public on the project too)
--   - internal: empty cidrs[] at install time, admin fills in private VPC
--     ranges via the Settings page
INSERT INTO cidr_presets (name, label, description, cidrs, is_system)
VALUES
    ('public',   '公开访问', '不做 IP 限制；任何能解析到 ingress 的来源都能访问。', '{}', true),
    ('internal', '内部访问', '只允许公司内网段访问；首次安装后请管理员去 设置 → 访问预设 填入实际 CIDR。', '{}', true)
ON CONFLICT (name) DO NOTHING;
