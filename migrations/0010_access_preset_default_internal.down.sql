-- Restore the nullable default. We do NOT undo the UPDATE — flipping projects
-- back to custom-with-empty-CIDRs would silently open access for everyone.
-- Operators wanting custom mode for a project can switch via the access panel.
ALTER TABLE projects ALTER COLUMN access_preset DROP DEFAULT;
