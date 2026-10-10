package backlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pikoolev/backlog-killer/internal/store"
)

type BulkInput struct {
	TicketIDs []string `json:"ticketIds"`
	Action    string   `json:"action"`
	Priority  *int     `json:"priority"`
	DueDate   *string  `json:"dueDate"`
}

func (s *Service) Comment(ctx context.Context, tenant, project, id string, version int64, a Actor, body string) (Ticket, error) {
	var out Ticket
	if body == "" || len(body) > 4000 {
		return out, &Error{"invalid_comment", 400, "comment must be 1..4000 characters"}
	}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		if err := scanTicket(tx.QueryRow(ctx, `SELECT t.id,t.tenant_id,t.project_id,t.text,t.specification,t.priority,t.due_date,t.status,t.version,p.url_path,COALESCE(p.url_pattern,''),p.title,t.anchor,t.selection,t.context,t.created_at,t.updated_at FROM tickets t JOIN pages p ON (p.tenant_id,p.project_id,p.id)=(t.tenant_id,t.project_id,t.page_id) WHERE t.project_id=$1 AND t.id=$2 FOR UPDATE OF t`, project, id), &out); err != nil {
			return err
		}
		if out.Version != version {
			return &Error{"stale_version", 412, "ticket version changed"}
		}
		if _, err := tx.Exec(ctx, `UPDATE tickets SET version=version+1,updated_at=now() WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenant, project, id); err != nil {
			return err
		}
		if err := event(ctx, tx, tenant, project, id, "comment", a, "", "", map[string]any{"body": body}); err != nil {
			return err
		}
		out.Version++
		out.UpdatedAt = time.Now()
		return nil
	})
	return out, err
}
func (s *Service) Bulk(ctx context.Context, tenant, project, principal, key string, a Actor, input BulkInput) error {
	if len(input.TicketIDs) < 1 || len(input.TicketIDs) > 200 {
		return &Error{"invalid_request", 400, "bulk requires 1..200 tickets"}
	}
	if input.Action != "set_due_date" && input.Action != "set_priority" && input.Action != "delete" {
		return &Error{"invalid_request", 400, "unsupported bulk action"}
	}
	if key == "" {
		return &Error{"idempotency_key_required", 400, "Idempotency-Key required"}
	}
	return store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		requestHash := HashRequest(input)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, principal+":"+project+":"+key); err != nil {
			return err
		}
		var storedHash []byte
		lookup := tx.QueryRow(ctx, `SELECT request_hash FROM idempotency_keys WHERE project_id=$1 AND principal=$2 AND key=$3`, project, principal, key).Scan(&storedHash)
		if lookup == nil {
			if !bytes.Equal(storedHash, requestHash[:]) {
				return &Error{"idempotency_conflict", 409, "Idempotency-Key reused with different payload"}
			}
			return nil
		}
		if !errors.Is(lookup, pgx.ErrNoRows) {
			return lookup
		}
		rows, err := tx.Query(ctx, `SELECT id,status FROM tickets WHERE project_id=$1 AND id=ANY($2::uuid[]) FOR UPDATE`, project, input.TicketIDs)
		if err != nil {
			return err
		}
		found := map[string]Status{}
		for rows.Next() {
			var id string
			var status Status
			if err = rows.Scan(&id, &status); err != nil {
				return err
			}
			found[id] = status
		}
		rows.Close()
		if len(found) != len(input.TicketIDs) {
			return ErrNotFound
		}
		for _, status := range found {
			if status == Done || status == Deleted {
				return &Error{"invalid_transition", 409, "closed tickets cannot be changed by bulk operation"}
			}
		}
		for _, id := range input.TicketIDs {
			metadata := map[string]any{"action": input.Action}
			switch input.Action {
			case "set_priority":
				if input.Priority == nil || *input.Priority < 1 || *input.Priority > 5 {
					return &Error{"invalid_request", 400, "priority required"}
				}
				_, err = tx.Exec(ctx, `UPDATE tickets SET priority=$1,version=version+1,updated_at=now() WHERE tenant_id=$2 AND project_id=$3 AND id=$4`, *input.Priority, tenant, project, id)
				metadata["priority"] = *input.Priority
			case "set_due_date":
				_, err = tx.Exec(ctx, `UPDATE tickets SET due_date=$1,version=version+1,updated_at=now() WHERE tenant_id=$2 AND project_id=$3 AND id=$4`, input.DueDate, tenant, project, id)
				metadata["dueDate"] = input.DueDate
			case "delete":
				_, err = tx.Exec(ctx, `UPDATE tickets SET previous_status=status,status='deleted',deleted_at=now(),version=version+1,updated_at=now() WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenant, project, id)
			}
			if err != nil {
				return err
			}
			if err = event(ctx, tx, tenant, project, id, "bulk", a, found[id], func() Status {
				if input.Action == "delete" {
					return Deleted
				}
				return found[id]
			}(), metadata); err != nil {
				return err
			}
		}
		response, _ := json.Marshal(map[string]any{"updated": len(input.TicketIDs)})
		_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(tenant_id,project_id,principal,key,request_hash,status_code,response) VALUES($1,$2,$3,$4,$5,200,$6)`, tenant, project, principal, key, requestHash[:], response)
		return err
	})
}
func CanonicalResponse(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
