-- Per-project ingress IP allow-list. Empty array = open access.
-- The reconciler reads this on every deploy and writes a Traefik IPAllowList
-- Middleware referenced by the project's Ingress.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS allow_cidrs text[] NOT NULL DEFAULT '{}';
