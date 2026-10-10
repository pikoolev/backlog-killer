CREATE OR REPLACE FUNCTION create_project_for_owner(lookup_issuer text,lookup_subject text,lookup_tenant uuid,project_name text,project_key_value text)
RETURNS TABLE(tenant_id uuid,project_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$
DECLARE uid uuid; pid uuid:=gen_random_uuid();
BEGIN
 SELECT u.id INTO uid FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id AND tm.tenant_id=lookup_tenant AND tm.is_owner WHERE u.issuer=lookup_issuer AND u.subject=lookup_subject;
 IF uid IS NULL THEN RETURN; END IF;
 INSERT INTO projects(id,tenant_id,project_key,name) VALUES(pid,lookup_tenant,project_key_value,project_name);
 INSERT INTO project_memberships(tenant_id,project_id,user_id,role) VALUES(lookup_tenant,pid,uid,'admin');
 RETURN QUERY SELECT lookup_tenant,pid;
END $$;
REVOKE ALL ON FUNCTION create_project_for_owner(text,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_project_for_owner(text,text,uuid,text,text) TO backlog_runtime;
