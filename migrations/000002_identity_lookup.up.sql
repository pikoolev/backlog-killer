CREATE TABLE embed_auth_flows (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_key text NOT NULL,
  origin text NOT NULL,
  state_hash bytea NOT NULL,
  code_challenge text NOT NULL,
  subject text,
  authorization_code_hash bytea,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz
);
ALTER TABLE embed_auth_flows ENABLE ROW LEVEL SECURITY;
ALTER TABLE embed_auth_flows FORCE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION authenticate_agent_token(lookup_prefix text)
RETURNS TABLE(id uuid,tenant_id uuid,project_id uuid,prefix text,secret_hash bytea,scopes text[],expires_at timestamptz,revoked_at timestamptz)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
  SELECT id,tenant_id,project_id,prefix,secret_hash,scopes,expires_at,revoked_at
  FROM agent_tokens WHERE prefix=lookup_prefix;
$$;

CREATE OR REPLACE FUNCTION resolve_project_identity(lookup_issuer text,lookup_subject text,lookup_project uuid)
RETURNS TABLE(user_id uuid,tenant_id uuid,project_id uuid,role text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
  SELECT u.id,p.tenant_id,p.id,
    CASE WHEN tm.is_owner THEN 'owner' ELSE pm.role::text END
  FROM users u
  JOIN project_memberships pm ON pm.user_id=u.id AND pm.project_id=lookup_project
  JOIN projects p ON (p.tenant_id,p.id)=(pm.tenant_id,pm.project_id)
  JOIN tenant_memberships tm ON tm.tenant_id=p.tenant_id AND tm.user_id=u.id
  WHERE u.issuer=lookup_issuer AND u.subject=lookup_subject;
$$;

REVOKE ALL ON FUNCTION authenticate_agent_token(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION resolve_project_identity(text,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION authenticate_agent_token(text),resolve_project_identity(text,text,uuid) TO backlog_runtime;
