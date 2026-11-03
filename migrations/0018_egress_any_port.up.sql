-- port 0 = any port. Domains: squid allows CONNECT to any port for that name.
-- Endpoints: the NetworkPolicy peer carries no port restriction. The API still
-- refuses any-port endpoint rules that cover the platform host.
ALTER TABLE project_egress_rules DROP CONSTRAINT IF EXISTS project_egress_rules_port_range;
ALTER TABLE project_egress_rules ADD CONSTRAINT project_egress_rules_port_range CHECK (
    (kind = 'domain'   AND port IN (0, 80, 443)) OR
    (kind = 'endpoint' AND port BETWEEN 0 AND 65535)
);
