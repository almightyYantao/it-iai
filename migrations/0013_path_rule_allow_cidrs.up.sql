-- Per-path external sources.
--
-- Ingress is internet-reachable once the security group opens 443, and the
-- project allow-list (internal preset) is the network gate. A path rule may
-- additionally admit specific external sources — e.g. a SaaS webhook's egress
-- IPs — for that prefix only, never the whole project. Admin-only, and such a
-- rule must be in token mode. Empty = the rule uses the project allow-list.
ALTER TABLE project_path_rules
    ADD COLUMN IF NOT EXISTS allow_cidrs text[] NOT NULL DEFAULT '{}';
