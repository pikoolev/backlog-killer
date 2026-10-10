CREATE OR REPLACE FUNCTION accept_invitation(code_digest bytea,lookup_issuer text,lookup_subject text,lookup_email text)
RETURNS TABLE(tenant_id uuid,project_id uuid,role text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=public AS $$
DECLARE invite invitations%ROWTYPE; identity_id uuid;
BEGIN
 SELECT * INTO invite FROM invitations WHERE token_hash=code_digest AND accepted_at IS NULL AND expires_at>now() FOR UPDATE;
 IF NOT FOUND OR lower(invite.email)<>lower(lookup_email) THEN RETURN; END IF;
 INSERT INTO users(issuer,subject,email,verified_email) VALUES(lookup_issuer,lookup_subject,lookup_email,true)
 ON CONFLICT(issuer,subject) DO UPDATE SET email=EXCLUDED.email,verified_email=true RETURNING id INTO identity_id;
 INSERT INTO tenant_memberships(tenant_id,user_id,is_owner) VALUES(invite.tenant_id,identity_id,false) ON CONFLICT DO NOTHING;
 IF invite.project_id IS NOT NULL THEN
  INSERT INTO project_memberships(tenant_id,project_id,user_id,role) VALUES(invite.tenant_id,invite.project_id,identity_id,invite.role)
  ON CONFLICT ON CONSTRAINT project_memberships_pkey DO UPDATE SET role=EXCLUDED.role;
 END IF;
 UPDATE invitations SET accepted_at=now(),accepted_by=identity_id WHERE id=invite.id;
 RETURN QUERY SELECT invite.tenant_id,invite.project_id,invite.role::text;
END $$;
REVOKE ALL ON FUNCTION accept_invitation(bytea,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION accept_invitation(bytea,text,text,text) TO backlog_runtime;
