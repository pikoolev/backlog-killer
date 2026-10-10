CREATE OR REPLACE FUNCTION lookup_project_origin(lookup_key text,lookup_origin text)
RETURNS TABLE(tenant_id uuid,project_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 SELECT p.tenant_id,p.id FROM projects p JOIN project_origins o ON (o.tenant_id,o.project_id)=(p.tenant_id,p.id)
 WHERE p.project_key=lookup_key AND o.origin=lookup_origin;
$$;
CREATE OR REPLACE FUNCTION create_embed_flow(flow_id uuid,lookup_key text,lookup_origin text,state_digest bytea,challenge text)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 INSERT INTO embed_auth_flows(id,project_key,origin,state_hash,code_challenge,expires_at)
 SELECT flow_id,lookup_key,lookup_origin,state_digest,challenge,now()+interval '5 minutes'
 WHERE EXISTS(SELECT 1 FROM lookup_project_origin(lookup_key,lookup_origin));
$$;
CREATE OR REPLACE FUNCTION issue_embed_code(flow_id uuid,lookup_subject text,code_digest bytea)
RETURNS text LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 UPDATE embed_auth_flows SET subject=lookup_subject,authorization_code_hash=code_digest
 WHERE id=flow_id AND expires_at>now() AND consumed_at IS NULL RETURNING origin;
$$;
CREATE OR REPLACE FUNCTION consume_embed_code(lookup_key text,lookup_origin text,code_digest bytea)
RETURNS TABLE(code_challenge text,subject text)
LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 UPDATE embed_auth_flows SET consumed_at=now()
 WHERE project_key=lookup_key AND origin=lookup_origin AND authorization_code_hash=code_digest AND expires_at>now() AND consumed_at IS NULL
 RETURNING embed_auth_flows.code_challenge,embed_auth_flows.subject;
$$;
REVOKE ALL ON FUNCTION lookup_project_origin(text,text),create_embed_flow(uuid,text,text,bytea,text),issue_embed_code(uuid,text,bytea),consume_embed_code(text,text,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_project_origin(text,text),create_embed_flow(uuid,text,text,bytea,text),issue_embed_code(uuid,text,bytea),consume_embed_code(text,text,bytea) TO backlog_runtime;
