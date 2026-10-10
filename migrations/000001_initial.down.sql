DROP TABLE IF EXISTS storage_deletions,idempotency_keys,agent_batch_tickets CASCADE;
ALTER TABLE IF EXISTS tickets DROP CONSTRAINT IF EXISTS tickets_claimed_batch_fk;
DROP TABLE IF EXISTS agent_batches,agent_tokens,ticket_attachments,ticket_events,tickets,pages,invitations,project_memberships,project_origins,projects,tenant_memberships,users,tenants CASCADE;
DROP TYPE IF EXISTS actor_kind;
DROP TYPE IF EXISTS ticket_status;
DROP TYPE IF EXISTS project_role;
