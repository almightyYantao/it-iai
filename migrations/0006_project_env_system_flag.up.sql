-- project_env.system distinguishes user-edited env vars from platform-managed
-- ones (auto-provisioned DATABASE_URL etc). The API uses this to refuse
-- DELETE on system rows so a misclick doesn't strip a project of its
-- generated credentials.
ALTER TABLE project_env
    ADD COLUMN IF NOT EXISTS system boolean NOT NULL DEFAULT false;
