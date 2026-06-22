-- Make the "internal" CIDR preset the default for new projects' access mode.
-- Previously access_preset was nullable and `CreateProject` left it NULL,
-- which the UI surfaced as "Custom" mode with an empty CIDR list — i.e.
-- effectively open to anyone who could resolve the ingress. Defaulting to the
-- "internal" preset means new projects come up gated to the corp network out
-- of the box.
--
-- Existing projects in custom mode (access_preset IS NULL) all have empty
-- allow_cidrs lists on this cluster, so flipping them to "internal" tightens
-- access (no IPs lost) rather than expanding it. Owners who want a different
-- list can still switch back to custom + paste their own CIDRs.
ALTER TABLE projects ALTER COLUMN access_preset SET DEFAULT 'internal';
UPDATE projects SET access_preset = 'internal'
 WHERE deleted_at IS NULL AND access_preset IS NULL;
