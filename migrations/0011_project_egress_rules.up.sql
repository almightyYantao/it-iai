-- Per-project egress allow-list, maintained by platform admins.
--
-- Pod egress is denied by default (private networks already, public internet
-- once the egress proxy lands). This table is the only way back out, and it is
-- deliberately not self-service: projects declare nothing in their manifest,
-- admins add rows.
--
-- Two kinds, because two different mechanisms enforce them and neither can do
-- the other's job:
--
--   domain    HTTP/HTTPS only. Rendered into the egress proxy's per-project
--             ACL, matched against the hostname in the request (or in the
--             CONNECT for TLS — no interception, so no CA in the pod). A
--             leading dot means "and every subdomain": `.openai.com` covers
--             api.openai.com, `api.openai.com` covers only itself.
--
--   endpoint  Any TCP. Rendered into the project's NetworkPolicy as an ipBlock
--             plus port. Needed for anything that isn't HTTP — an internal
--             Postgres, gRPC, SMTP — because NetworkPolicy cannot express
--             names, only addresses.
--
-- The corollary matters and the UI has to say it out loud: putting a hostname
-- in for a non-HTTP service silently does nothing. It has to be an address.
CREATE TABLE IF NOT EXISTS project_egress_rules (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('domain', 'endpoint')),

    -- domain:   'api.anthropic.com' or '.anthropic.com' (lower-cased, no scheme,
    --           no path, no port)
    -- endpoint: an IP or CIDR in canonical form ('10.8.0.0/16', '203.0.113.7/32')
    value      text NOT NULL,

    -- Always set, so UNIQUE below actually constrains: NULLs compare distinct in
    -- Postgres, which would let the same destination be added over and over.
    -- Domains are limited to the two ports the proxy speaks; allowing arbitrary
    -- ports there would turn any allow-listed name into a tunnel
    -- (CONNECT allowed.example.com:22).
    port       integer NOT NULL,

    -- Why this rule exists / who asked. Free text for the admin's own audit.
    note       text NOT NULL DEFAULT '',

    created_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid REFERENCES users(id),

    CONSTRAINT project_egress_rules_port_range CHECK (
        (kind = 'domain'   AND port IN (80, 443)) OR
        (kind = 'endpoint' AND port BETWEEN 1 AND 65535)
    ),
    UNIQUE (project_id, kind, value, port)
);

CREATE INDEX IF NOT EXISTS project_egress_rules_project_idx
    ON project_egress_rules(project_id);
