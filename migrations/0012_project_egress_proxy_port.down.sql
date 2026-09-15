DROP INDEX IF EXISTS projects_egress_proxy_port_live_idx;
ALTER TABLE projects DROP COLUMN IF EXISTS egress_proxy_port;
