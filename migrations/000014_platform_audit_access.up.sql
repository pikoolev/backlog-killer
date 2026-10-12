GRANT SELECT,INSERT ON platform_admin_audit TO backlog_admin;
SET LOCAL ROLE backlog_admin;

CREATE OR REPLACE FUNCTION platform_admin_mask_project(target_project uuid,target_user uuid DEFAULT NULL)
RETURNS TABLE(user_id uuid,tenant_id uuid,project_id uuid,project_key text,project_name text,role text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT COALESCE(target_user,owner.user_id),p.tenant_id,p.id,p.project_key,p.name,'owner'::text
 FROM projects p
 LEFT JOIN LATERAL (
   SELECT tm.user_id FROM tenant_memberships tm
   WHERE tm.tenant_id=p.tenant_id
   ORDER BY tm.is_owner DESC,tm.created_at LIMIT 1
 ) owner ON true
 WHERE p.id=target_project AND COALESCE(target_user,owner.user_id) IS NOT NULL;
$$;
ALTER FUNCTION platform_admin_mask_project(uuid,uuid) OWNER TO backlog_admin;
REVOKE ALL ON FUNCTION platform_admin_mask_project(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform_admin_mask_project(uuid,uuid) TO backlog_runtime;
