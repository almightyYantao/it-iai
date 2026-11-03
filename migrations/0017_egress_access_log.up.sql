-- Every request the egress proxy saw, kept 30 days (pruned by the collector).
-- Host only, never the full URL: query strings can carry secrets.
CREATE TABLE IF NOT EXISTS egress_access_log (
    id         bigserial PRIMARY KEY,
    ts         timestamptz NOT NULL,
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    slug       text NOT NULL,
    method     text NOT NULL,
    host       text NOT NULL,
    status     text NOT NULL,
    denied     boolean NOT NULL
);
CREATE INDEX IF NOT EXISTS egress_access_log_ts_idx ON egress_access_log (ts DESC);
CREATE INDEX IF NOT EXISTS egress_access_log_proj_ts_idx ON egress_access_log (project_id, ts DESC);

-- The control-plane connects as iai_cp; migrations run as the owner. Grant the
-- tables added since, and make future ones follow automatically.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'iai_cp') THEN
    GRANT SELECT, INSERT, UPDATE, DELETE ON egress_domain_stats, egress_access_log TO iai_cp;
    GRANT USAGE, SELECT ON SEQUENCE egress_access_log_id_seq TO iai_cp;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO iai_cp;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO iai_cp;
  END IF;
END $$;
