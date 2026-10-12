GRANT USAGE,CREATE ON SCHEMA public TO backlog_admin;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO backlog_admin;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO backlog_admin;




CREATE TYPE tenant_role AS ENUM ('owner','admin','member');
ALTER TABLE tenant_memberships ADD COLUMN role tenant_role NOT NULL DEFAULT 'member';
UPDATE tenant_memberships SET role=CASE WHEN is_owner THEN 'owner'::tenant_role ELSE 'member'::tenant_role END;

CREATE OR REPLACE FUNCTION list_user_projects(lookup_issuer text,lookup_subject text)
RETURNS TABLE(tenant_id uuid,project_id uuid,project_key text,project_name text,role text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT p.tenant_id,p.id,p.project_key,p.name,CASE tm.role WHEN 'owner' THEN 'owner' WHEN 'admin' THEN 'admin' ELSE 'reporter' END
 FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id JOIN projects p ON p.tenant_id=tm.tenant_id
 WHERE u.issuer=lookup_issuer AND u.subject=lookup_subject ORDER BY p.name,p.id;
$$;
ALTER FUNCTION list_user_projects(text,text) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION resolve_project_identity(lookup_issuer text,lookup_subject text,lookup_project uuid)
RETURNS TABLE(user_id uuid,tenant_id uuid,project_id uuid,role text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT u.id,p.tenant_id,p.id,CASE tm.role WHEN 'owner' THEN 'owner' WHEN 'admin' THEN 'admin' ELSE 'reporter' END
 FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id JOIN projects p ON p.tenant_id=tm.tenant_id
 WHERE u.issuer=lookup_issuer AND u.subject=lookup_subject AND p.id=lookup_project;
$$;
ALTER FUNCTION resolve_project_identity(text,text,uuid) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION create_tenant_with_owner(lookup_issuer text,lookup_subject text,lookup_email text,tenant_name text,project_name text,project_key_value text)
RETURNS TABLE(tenant_id uuid,project_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$
DECLARE uid uuid; tid uuid:=gen_random_uuid(); pid uuid:=gen_random_uuid(); BEGIN
 INSERT INTO users(issuer,subject,email,verified_email) VALUES(lookup_issuer,lookup_subject,NULLIF(lookup_email,''),lookup_email<>'') ON CONFLICT(issuer,subject) DO UPDATE SET email=COALESCE(EXCLUDED.email,users.email) RETURNING id INTO uid;
 INSERT INTO tenants(id,name) VALUES(tid,tenant_name);
 INSERT INTO tenant_memberships(tenant_id,user_id,is_owner,role) VALUES(tid,uid,true,'owner');
 INSERT INTO projects(id,tenant_id,project_key,name) VALUES(pid,tid,project_key_value,project_name);
 INSERT INTO project_memberships(tenant_id,project_id,user_id,role) VALUES(tid,pid,uid,'admin');
 RETURN QUERY SELECT tid,pid;
END $$;
GRANT EXECUTE ON FUNCTION list_user_projects(text,text),resolve_project_identity(text,text,uuid),create_tenant_with_owner(text,text,text,text,text,text) TO backlog_runtime;
ALTER FUNCTION create_tenant_with_owner(text,text,text,text,text,text) OWNER TO backlog_admin;

CREATE TABLE platform_admin_audit (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  admin_issuer text NOT NULL,
  admin_subject text NOT NULL,
  action text NOT NULL,
  tenant_id uuid,
  project_id uuid,
  target_user_id uuid,
  metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);

ALTER FUNCTION create_project_for_owner(text,text,uuid,text,text) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_catalog()
RETURNS jsonb LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT COALESCE(jsonb_agg(jsonb_build_object(
   'id',t.id,'name',t.name,
   'projects',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',p.id,'name',p.name,'projectKey',p.project_key) ORDER BY p.name) FROM projects p WHERE p.tenant_id=t.id),'[]'::jsonb),
   'users',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',u.id,'email',COALESCE(u.email,''),'name',u.subject,'role',tm.role::text) ORDER BY u.email) FROM tenant_memberships tm JOIN users u ON u.id=tm.user_id WHERE tm.tenant_id=t.id),'[]'::jsonb)
 ) ORDER BY t.name),'[]'::jsonb) FROM tenants t;
$$;
ALTER FUNCTION platform_admin_catalog() OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_update_tenant(target_tenant uuid,new_name text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$ BEGIN
 IF trim(new_name)='' THEN RAISE EXCEPTION 'tenant name required'; END IF;
 UPDATE tenants SET name=new_name WHERE id=target_tenant;
 IF NOT FOUND THEN RAISE EXCEPTION 'tenant not found'; END IF;
END $$;
ALTER FUNCTION platform_admin_update_tenant(uuid,text) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_create_project(target_tenant uuid,new_name text,new_key text)
RETURNS TABLE(tenant_id uuid,project_id uuid) LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$
DECLARE pid uuid:=gen_random_uuid(); BEGIN
 INSERT INTO projects(id,tenant_id,project_key,name) VALUES(pid,target_tenant,new_key,new_name);
 RETURN QUERY SELECT target_tenant,pid;
END $$;
ALTER FUNCTION platform_admin_create_project(uuid,text,text) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_update_project(target_project uuid,new_name text,new_key text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$ BEGIN
 UPDATE projects SET name=new_name,project_key=new_key WHERE id=target_project;
 IF NOT FOUND THEN RAISE EXCEPTION 'project not found'; END IF;
END $$;
ALTER FUNCTION platform_admin_update_project(uuid,text,text) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_upsert_user(target_tenant uuid,user_issuer text,user_subject text,user_email text,user_role tenant_role)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$
DECLARE uid uuid; BEGIN
 INSERT INTO users(issuer,subject,email,verified_email) VALUES(user_issuer,user_subject,user_email,true)
 ON CONFLICT(issuer,subject) DO UPDATE SET email=EXCLUDED.email,verified_email=true RETURNING id INTO uid;
 INSERT INTO tenant_memberships(tenant_id,user_id,is_owner,role) VALUES(target_tenant,uid,user_role='owner',user_role)
 ON CONFLICT(tenant_id,user_id) DO UPDATE SET is_owner=EXCLUDED.is_owner,role=EXCLUDED.role;
 RETURN uid;
END $$;
ALTER FUNCTION platform_admin_upsert_user(uuid,text,text,text,tenant_role) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_remove_user(target_tenant uuid,target_user uuid)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$ BEGIN
 DELETE FROM project_memberships WHERE tenant_id=target_tenant AND user_id=target_user;
 DELETE FROM tenant_memberships WHERE tenant_id=target_tenant AND user_id=target_user;
END $$;
ALTER FUNCTION platform_admin_remove_user(uuid,uuid) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_mask_project(target_project uuid,target_user uuid DEFAULT NULL)
RETURNS TABLE(user_id uuid,tenant_id uuid,project_id uuid,project_key text,project_name text,role text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT COALESCE(target_user,(SELECT tm.user_id FROM tenant_memberships tm WHERE tm.tenant_id=p.tenant_id ORDER BY tm.is_owner DESC,tm.created_at LIMIT 1)),p.tenant_id,p.id,p.project_key,p.name,'owner'::text
 FROM projects p WHERE p.id=target_project;
$$;
ALTER FUNCTION platform_admin_mask_project(uuid,uuid) OWNER TO backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_log(admin_issuer text,admin_subject text,admin_action text,target_tenant uuid,target_project uuid,target_user uuid,details jsonb)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 INSERT INTO platform_admin_audit(admin_issuer,admin_subject,action,tenant_id,project_id,target_user_id,metadata) VALUES(admin_issuer,admin_subject,admin_action,target_tenant,target_project,target_user,COALESCE(details,'{}'));
$$;
ALTER FUNCTION platform_admin_log(text,text,text,uuid,uuid,uuid,jsonb) OWNER TO backlog_admin;

REVOKE ALL ON FUNCTION platform_admin_catalog(),platform_admin_update_tenant(uuid,text),platform_admin_create_project(uuid,text,text),platform_admin_update_project(uuid,text,text),platform_admin_upsert_user(uuid,text,text,text,tenant_role),platform_admin_remove_user(uuid,uuid),platform_admin_mask_project(uuid,uuid),platform_admin_log(text,text,text,uuid,uuid,uuid,jsonb) FROM PUBLIC;
SET LOCAL ROLE backlog_admin;
GRANT EXECUTE ON FUNCTION platform_admin_catalog(),platform_admin_update_tenant(uuid,text),platform_admin_create_project(uuid,text,text),platform_admin_update_project(uuid,text,text),platform_admin_upsert_user(uuid,text,text,text,tenant_role),platform_admin_remove_user(uuid,uuid),platform_admin_mask_project(uuid,uuid),platform_admin_log(text,text,text,uuid,uuid,uuid,jsonb) TO backlog_runtime;
