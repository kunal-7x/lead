-- +goose Up

-- Enable RLS on all tenant-scoped tables.
ALTER TABLE tenant_users  ENABLE ROW LEVEL SECURITY;
ALTER TABLE roles          ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_tenant_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys       ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs     ENABLE ROW LEVEL SECURITY;

-- Helper: current tenant from session variable (set by app on every connection).
CREATE OR REPLACE FUNCTION current_tenant_id() RETURNS UUID AS $$
  SELECT NULLIF(current_setting('app.current_tenant_id', TRUE), '')::UUID;
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION current_user_id() RETURNS UUID AS $$
  SELECT NULLIF(current_setting('app.current_user_id', TRUE), '')::UUID;
$$ LANGUAGE SQL STABLE;

-- tenant_users: read/write only rows belonging to the current tenant.
CREATE POLICY tenant_users_isolation ON tenant_users
    USING (tenant_id = current_tenant_id());

-- roles
CREATE POLICY roles_isolation ON roles
    USING (tenant_id = current_tenant_id());

-- user_tenant_roles
CREATE POLICY utr_isolation ON user_tenant_roles
    USING (tenant_id = current_tenant_id());

-- api_keys
CREATE POLICY api_keys_isolation ON api_keys
    USING (tenant_id = current_tenant_id());

-- audit_logs: read-own-tenant; no UPDATE/DELETE for any role (append-only).
CREATE POLICY audit_logs_read ON audit_logs
    FOR SELECT USING (tenant_id = current_tenant_id());

CREATE POLICY audit_logs_insert ON audit_logs
    FOR INSERT WITH CHECK (tenant_id = current_tenant_id());

-- Superuser (migrations user) bypasses RLS.
-- The app connects as a role with BYPASSRLS for admin ops;
-- tenant-scoped operations use a restricted role.

-- +goose Down
DROP POLICY IF EXISTS audit_logs_insert  ON audit_logs;
DROP POLICY IF EXISTS audit_logs_read    ON audit_logs;
DROP POLICY IF EXISTS api_keys_isolation ON api_keys;
DROP POLICY IF EXISTS utr_isolation      ON user_tenant_roles;
DROP POLICY IF EXISTS roles_isolation    ON roles;
DROP POLICY IF EXISTS tenant_users_isolation ON tenant_users;

DROP FUNCTION IF EXISTS current_user_id();
DROP FUNCTION IF EXISTS current_tenant_id();

ALTER TABLE audit_logs        DISABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys          DISABLE ROW LEVEL SECURITY;
ALTER TABLE user_tenant_roles DISABLE ROW LEVEL SECURITY;
ALTER TABLE roles             DISABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_users      DISABLE ROW LEVEL SECURITY;
