-- Domains seen by the egress proxy, aggregated per project. The proxy's own log
-- is lost every time its pod is recreated (any config change), so the
-- control-plane folds it into this table.
CREATE TABLE IF NOT EXISTS egress_domain_stats (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    host       text NOT NULL,
    allowed    bigint NOT NULL DEFAULT 0,
    denied     bigint NOT NULL DEFAULT 0,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, host)
);
