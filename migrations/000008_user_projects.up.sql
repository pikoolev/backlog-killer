CREATE OR REPLACE FUNCTION list_user_projects(lookup_issuer text, lookup_subject text)
RETURNS TABLE(tenant_id uuid, project_id uuid, project_key text, project_name text, role text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
  SELECT p.tenant_id,p.id,p.project_key,p.name,
    CASE WHEN tm.is_owner THEN 'owner' ELSE pm.role::text END
  FROM users u
  JOIN tenant_memberships tm ON tm.user_id=u.id
  JOIN project_memberships pm ON pm.tenant_id=tm.tenant_id AND pm.user_id=u.id
  JOIN projects p ON (p.tenant_id,p.id)=(pm.tenant_id,pm.project_id)
  WHERE u.issuer=lookup_issuer AND u.subject=lookup_subject
  ORDER BY p.name,p.id;
$$;
REVOKE ALL ON FUNCTION list_user_projects(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION list_user_projects(text,text) TO backlog_runtime;
