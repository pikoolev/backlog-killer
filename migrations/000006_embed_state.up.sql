ALTER TABLE embed_auth_flows ADD COLUMN state_value text NOT NULL DEFAULT '';
CREATE OR REPLACE FUNCTION create_embed_flow(flow_id uuid,lookup_key text,lookup_origin text,state_digest bytea,state_value text,challenge text)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 INSERT INTO embed_auth_flows(id,project_key,origin,state_hash,state_value,code_challenge,expires_at)
 SELECT flow_id,lookup_key,lookup_origin,state_digest,state_value,challenge,now()+interval '5 minutes'
 WHERE EXISTS(SELECT 1 FROM lookup_project_origin(lookup_key,lookup_origin));
$$;
DROP FUNCTION create_embed_flow(uuid,text,text,bytea,text);
DROP FUNCTION issue_embed_code(uuid,text,bytea);
CREATE FUNCTION issue_embed_code(flow_id uuid,lookup_subject text,code_digest bytea)
RETURNS TABLE(origin text,state_value text) LANGUAGE sql SECURITY DEFINER SET search_path=public AS $$
 UPDATE embed_auth_flows SET subject=lookup_subject,authorization_code_hash=code_digest
 WHERE id=flow_id AND expires_at>now() AND consumed_at IS NULL
 RETURNING embed_auth_flows.origin,embed_auth_flows.state_value;
$$;
REVOKE ALL ON FUNCTION create_embed_flow(uuid,text,text,bytea,text,text),issue_embed_code(uuid,text,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_embed_flow(uuid,text,text,bytea,text,text),issue_embed_code(uuid,text,bytea) TO backlog_runtime;
