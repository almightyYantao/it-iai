DELETE FROM project_egress_rules WHERE port = 0;
ALTER TABLE project_egress_rules DROP CONSTRAINT IF EXISTS project_egress_rules_port_range;
ALTER TABLE project_egress_rules ADD CONSTRAINT project_egress_rules_port_range CHECK (
    (kind = 'domain' AND port IN (80, 443)) OR (kind = 'endpoint' AND port BETWEEN 1 AND 65535));
