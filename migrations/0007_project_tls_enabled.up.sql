-- Per-project HTTPS opt-in. When true, the reconciler annotates the project's
-- Ingress with cert-manager.io/cluster-issuer so cert-manager solves an
-- HTTP-01 challenge and provisions a per-host certificate. Default off so
-- projects come up reachable over plain HTTP even before cert-manager is
-- installed in the cluster.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS tls_enabled boolean NOT NULL DEFAULT false;
