DROP FUNCTION IF EXISTS create_embed_flow(uuid,text,text,bytea,text,text);
ALTER TABLE embed_auth_flows DROP COLUMN IF EXISTS state_value;
