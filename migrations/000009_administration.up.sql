CREATE OR REPLACE FUNCTION create_tenant_with_owner(lookup_issuer text,lookup_subject text,lookup_email text,tenant_name text,project_name text,project_key_value text)
RETURNS TABLE(tenant_id uuid,project_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$
DECLARE uid uuid; tid uuid:=gen_random_uuid(); pid uuid:=gen_random_uuid();
BEGIN
  INSERT INTO users(issuer,subject,email,verified_email) VALUES(lookup_issuer,lookup_subject,NULLIF(lookup_email,''),lookup_email<>'') ON CONFLICT(issuer,subject) DO UPDATE SET email=COALESCE(EXCLUDED.email,users.email) RETURNING id INTO uid;
  INSERT INTO tenants(id,name) VALUES(tid,tenant_name);
  INSERT INTO tenant_memberships(tenant_id,user_id,is_owner) VALUES(tid,uid,true);
  INSERT INTO projects(id,tenant_id,project_key,name) VALUES(pid,tid,project_key_value,project_name);
  INSERT INTO project_memberships(tenant_id,project_id,user_id,role) VALUES(tid,pid,uid,'admin');
  RETURN QUERY SELECT tid,pid;
END $$;

CREATE OR REPLACE FUNCTION list_project_admin(lookup_issuer text,lookup_subject text,lookup_project uuid)
RETURNS TABLE(tenant_id uuid,project_id uuid,project_key text,project_name text,role text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT p.tenant_id,p.id,p.project_key,p.name,CASE WHEN tm.is_owner THEN 'owner' ELSE pm.role::text END
 FROM users u JOIN project_memberships pm ON pm.user_id=u.id AND pm.project_id=lookup_project
 JOIN projects p ON (p.tenant_id,p.id)=(pm.tenant_id,pm.project_id)
 JOIN tenant_memberships tm ON tm.tenant_id=p.tenant_id AND tm.user_id=u.id
 WHERE u.issuer=lookup_issuer AND u.subject=lookup_subject;
$$;
REVOKE ALL ON FUNCTION create_tenant_with_owner(text,text,text,text,text,text),list_project_admin(text,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_tenant_with_owner(text,text,text,text,text,text),list_project_admin(text,text,uuid) TO backlog_runtime;
CREATE INDEX IF NOT EXISTS ticket_events_ticket_idx ON ticket_events(tenant_id,project_id,ticket_id,created_at,id);
CREATE INDEX IF NOT EXISTS ticket_attachments_ticket_idx ON ticket_attachments(tenant_id,project_id,ticket_id,created_at,id);
