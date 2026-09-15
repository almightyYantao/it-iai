-- Per-project listening port on the egress proxy.
--
-- The proxy has to know which project a connection belongs to in order to apply
-- that project's domain allow-list, and the options for telling it are: source
-- IP (pod IPs are dynamic and flannel assigns per-node, not per-namespace, so
-- there is nothing stable to match), proxy authentication (a shared secret in
-- every pod, and some SDKs mishandle CONNECT with auth), or a dedicated port
-- per project. The port is not a secret, needs no rotation, and squid matches it
-- natively with the myportname ACL — so: a port.
--
-- Allocated lazily on first need and never reused while the project is live, so
-- a pod holding a stale HTTPS_PROXY value can't inherit another project's
-- allow-list. Kept out of the 1-1024 range and clear of the platform's own
-- published ports.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS egress_proxy_port integer;

-- Partial unique: only live projects need a distinct port, and a tombstoned row
-- shouldn't keep one reserved forever.
CREATE UNIQUE INDEX IF NOT EXISTS projects_egress_proxy_port_live_idx
    ON projects(egress_proxy_port)
    WHERE deleted_at IS NULL AND egress_proxy_port IS NOT NULL;
