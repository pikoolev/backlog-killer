CREATE OR REPLACE FUNCTION lookup_project_id_origin(lookup_project uuid,lookup_origin text)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT EXISTS(SELECT 1 FROM project_origins WHERE project_id=lookup_project AND origin=lookup_origin);
$$;
REVOKE ALL ON FUNCTION lookup_project_id_origin(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_project_id_origin(uuid,text) TO backlog_runtime;
