package backlog

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pikoolev/backlog-killer/internal/store"
)

type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	ActorKind string         `json:"actorKind"`
	ActorID   string         `json:"actorId"`
	ActorName string         `json:"actorName"`
	From      *Status        `json:"fromStatus"`
	To        *Status        `json:"toStatus"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"createdAt"`
}

type Attachment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MediaType string    `json:"mediaType"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"createdAt"`
}

type Batch struct {
	ID           string    `json:"id"`
	OwnerSubject string    `json:"ownerSubject"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Detail struct {
	Ticket
	Events      []Event      `json:"events"`
	Attachments []Attachment `json:"attachments"`
	Batch       *Batch       `json:"batch"`
	Permissions []string     `json:"permissions"`
}

type UpdateInput struct {
	Text          *string `json:"text"`
	Specification *string `json:"specification"`
	Priority      *int    `json:"priority"`
	DueDate       *string `json:"dueDate"`
	Status        *Status `json:"status"`
}

func Permissions(a Actor, status Status) []string {
	out := []string{"read"}
	if a.Kind == "agent" {
		if a.Scopes["tickets:comment"] {
			out = append(out, "comment")
		}
		if a.Scopes["tickets:complete"] && (status == InProgress || status == Rework) {
			out = append(out, "submit_review")
		}
		return out
	}
	out = append(out, "comment")
	if a.Role == "owner" || a.Role == "admin" || a.Role == "triager" {
		out = append(out, "update")
		if status != Deleted && status != Done {
			out = append(out, "delete")
		}
		if status == Deleted {
			out = append(out, "restore")
		}
	}
	if (a.Role == "owner" || a.Role == "admin") && status == Review {
		out = append(out, "accept", "rework")
	}
	return out
}

func (s *Service) Detail(ctx context.Context, tenant, project, id string, a Actor) (Detail, error) {
	var out Detail
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		if err := scanTicket(tx.QueryRow(ctx, ticketSelect+` WHERE t.project_id=$1 AND t.id=$2`, project, id), &out.Ticket); err != nil {
			return err
		}
		out.Permissions = Permissions(a, out.Status)
		out.Events = []Event{}
		rows, err := tx.Query(ctx, `SELECT id,type,actor_kind,actor_id,actor_name,from_status,to_status,metadata,created_at FROM ticket_events WHERE project_id=$1 AND ticket_id=$2 ORDER BY created_at,id`, project, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item Event
			var metadata []byte
			if err = rows.Scan(&item.ID, &item.Type, &item.ActorKind, &item.ActorID, &item.ActorName, &item.From, &item.To, &metadata, &item.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			_ = json.Unmarshal(metadata, &item.Metadata)
			out.Events = append(out.Events, item)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		out.Attachments = []Attachment{}
		rows, err = tx.Query(ctx, `SELECT id,display_name,media_type,size_bytes,encode(sha256,'hex'),created_at FROM ticket_attachments WHERE project_id=$1 AND ticket_id=$2 AND deleted_at IS NULL ORDER BY created_at,id`, project, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item Attachment
			if err = rows.Scan(&item.ID, &item.Name, &item.MediaType, &item.Size, &item.SHA256, &item.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			out.Attachments = append(out.Attachments, item)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		var batch Batch
		err = tx.QueryRow(ctx, `SELECT b.id,b.owner_subject,b.created_at FROM agent_batches b JOIN agent_batch_tickets bt ON (bt.tenant_id,bt.project_id,bt.batch_id)=(b.tenant_id,b.project_id,b.id) WHERE bt.project_id=$1 AND bt.ticket_id=$2 AND bt.completed_at IS NULL AND b.active`, project, id).Scan(&batch.ID, &batch.OwnerSubject, &batch.CreatedAt)
		if err == nil {
			out.Batch = &batch
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	return out, err
}

const ticketSelect = `SELECT t.id,t.tenant_id,t.project_id,t.text,t.specification,t.priority,t.due_date,t.status,t.version,p.url_path,COALESCE(p.url_pattern,''),p.title,t.anchor,t.selection,t.context,t.created_at,t.updated_at FROM tickets t JOIN pages p ON (p.tenant_id,p.project_id,p.id)=(t.tenant_id,t.project_id,t.page_id)`

func (s *Service) Update(ctx context.Context, tenant, project, id string, version int64, a Actor, in UpdateInput) (Ticket, error) {
	var out Ticket
	if a.Kind == "agent" || (a.Role != "owner" && a.Role != "admin" && a.Role != "triager") {
		return out, &Error{"forbidden", 403, "triage role required"}
	}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		if err := scanTicket(tx.QueryRow(ctx, ticketSelect+` WHERE t.project_id=$1 AND t.id=$2 FOR UPDATE OF t`, project, id), &out); err != nil {
			return err
		}
		if out.Version != version {
			return &Error{"stale_version", 412, "ticket version changed"}
		}
		if out.Status == Deleted || out.Status == Done {
			return &Error{"invalid_transition", 409, "closed ticket cannot be edited"}
		}
		if in.Text != nil && (len(*in.Text) < 1 || len(*in.Text) > 4000) {
			return &Error{"invalid_request", 400, "text must be 1..4000 characters"}
		}
		if in.Specification != nil && len(*in.Specification) > 20000 {
			return &Error{"invalid_request", 400, "specification exceeds 20000 characters"}
		}
		if in.Priority != nil && (*in.Priority < 1 || *in.Priority > 5) {
			return &Error{"invalid_request", 400, "priority must be 1..5"}
		}
		var due any
		if in.DueDate != nil {
			if *in.DueDate != "" {
				parsed, e := time.Parse("2006-01-02", *in.DueDate)
				if e != nil {
					return &Error{"invalid_due_date", 400, "dueDate must be YYYY-MM-DD"}
				}
				due = parsed
			}
		}
		to := out.Status
		if in.Status != nil {
			to = *in.Status
			if !((out.Status == New && to == Clarification) || (out.Status == Clarification && to == New) || ((out.Status == New || out.Status == Clarification) && to == InProgress)) {
				return &Error{"invalid_transition", 409, "triage transition is not allowed"}
			}
		}
		_, err := tx.Exec(ctx, `UPDATE tickets SET text=COALESCE($1,text),specification=COALESCE($2,specification),priority=COALESCE($3,priority),due_date=CASE WHEN $4::boolean THEN $5::date ELSE due_date END,status=$6,version=version+1,updated_at=now() WHERE tenant_id=$7 AND project_id=$8 AND id=$9`, in.Text, in.Specification, in.Priority, in.DueDate != nil, due, to, tenant, project, id)
		if err != nil {
			return err
		}
		meta := map[string]any{"textChanged": in.Text != nil, "specificationChanged": in.Specification != nil}
		if in.Priority != nil {
			meta["priority"] = *in.Priority
		}
		if in.DueDate != nil {
			meta["dueDate"] = *in.DueDate
		}
		if err = event(ctx, tx, tenant, project, id, "triage", a, out.Status, to, meta); err != nil {
			return err
		}
		if in.Text != nil {
			out.Text = *in.Text
		}
		if in.Specification != nil {
			out.Specification = *in.Specification
		}
		if in.Priority != nil {
			out.Priority = *in.Priority
		}
		out.Status = to
		out.Version++
		out.UpdatedAt = time.Now()
		return nil
	})
	return out, err
}

func (s *Service) Delete(ctx context.Context, tenant, project, id string, version int64, a Actor) (Ticket, error) {
	return s.changeDeleted(ctx, tenant, project, id, version, a, false)
}
func (s *Service) Restore(ctx context.Context, tenant, project, id string, version int64, a Actor) (Ticket, error) {
	return s.changeDeleted(ctx, tenant, project, id, version, a, true)
}
func (s *Service) changeDeleted(ctx context.Context, tenant, project, id string, version int64, a Actor, restore bool) (Ticket, error) {
	var out Ticket
	if a.Kind == "agent" || (a.Role != "owner" && a.Role != "admin" && a.Role != "triager") {
		return out, &Error{"forbidden", 403, "triage role required"}
	}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		if err := scanTicket(tx.QueryRow(ctx, ticketSelect+` WHERE t.project_id=$1 AND t.id=$2 FOR UPDATE OF t`, project, id), &out); err != nil {
			return err
		}
		if out.Version != version {
			return &Error{"stale_version", 412, "ticket version changed"}
		}
		from := out.Status
		to := Deleted
		if restore {
			if from != Deleted {
				return &Error{"invalid_transition", 409, "ticket is not deleted"}
			}
			if err := tx.QueryRow(ctx, `SELECT previous_status FROM tickets WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenant, project, id).Scan(&to); err != nil {
				return err
			}
			if to == Deleted || to == Done {
				return &Error{"invalid_transition", 409, "ticket cannot be restored"}
			}
		} else if from == Deleted || from == Done {
			return &Error{"invalid_transition", 409, "ticket cannot be deleted"}
		}
		if !restore {
			if err := releaseTicketClaim(ctx, tx, tenant, project, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE tickets SET previous_status=CASE WHEN $1::boolean THEN previous_status ELSE status END,status=$2,deleted_at=CASE WHEN $1::boolean THEN NULL ELSE now() END,version=version+1,updated_at=now() WHERE tenant_id=$3 AND project_id=$4 AND id=$5`, restore, to, tenant, project, id)
		if err != nil {
			return err
		}
		if err = event(ctx, tx, tenant, project, id, map[bool]string{true: "restored", false: "deleted"}[restore], a, from, to, map[string]any{}); err != nil {
			return err
		}
		out.Status = to
		out.Version++
		out.UpdatedAt = time.Now()
		return nil
	})
	return out, err
}

func releaseTicketClaim(ctx context.Context, tx pgx.Tx, tenant, project, ticket string) error {
	var batchID *string
	if err := tx.QueryRow(ctx, `SELECT claimed_batch_id FROM tickets WHERE tenant_id=$1 AND project_id=$2 AND id=$3 FOR UPDATE`, tenant, project, ticket).Scan(&batchID); err != nil {
		return err
	}
	if batchID == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_batch_tickets SET completed_at=now() WHERE tenant_id=$1 AND project_id=$2 AND batch_id=$3 AND ticket_id=$4 AND completed_at IS NULL`, tenant, project, *batchID, ticket); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE tickets SET claimed_batch_id=NULL WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenant, project, ticket); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agent_batches b SET active=false,closed_at=now() WHERE b.tenant_id=$1 AND b.project_id=$2 AND b.id=$3 AND b.active AND NOT EXISTS (SELECT 1 FROM agent_batch_tickets bt WHERE bt.tenant_id=b.tenant_id AND bt.project_id=b.project_id AND bt.batch_id=b.id AND bt.completed_at IS NULL)`, tenant, project, *batchID)
	return err
}
