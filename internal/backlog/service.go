package backlog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pikoolev/backlog-killer/internal/store"
)

type Status string

const (
	New           Status = "new"
	Clarification Status = "needs_clarification"
	InProgress    Status = "in_progress"
	Review        Status = "review"
	Rework        Status = "rework"
	Done          Status = "done"
	Deleted       Status = "deleted"
)

type Actor struct {
	Kind, ID, Name, Role string
	Scopes               map[string]bool
}
type Page struct {
	URLPath    string `json:"urlPath"`
	URLPattern string `json:"urlPattern,omitempty"`
	Title      string `json:"title"`
}
type CreateInput struct {
	Text      string         `json:"text"`
	Priority  int            `json:"priority"`
	DueDate   *string        `json:"dueDate"`
	Page      Page           `json:"page"`
	Anchor    map[string]any `json:"anchor"`
	Selection map[string]any `json:"selection"`
	Context   map[string]any `json:"context"`
}
type Ticket struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenantId"`
	ProjectID     string         `json:"projectId"`
	Text          string         `json:"text"`
	Specification string         `json:"specification"`
	Priority      int            `json:"priority"`
	DueDate       *time.Time     `json:"dueDate"`
	Status        Status         `json:"status"`
	Version       int64          `json:"version"`
	Page          Page           `json:"page"`
	Anchor        map[string]any `json:"anchor"`
	Selection     map[string]any `json:"selection"`
	Context       map[string]any `json:"context"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}
type TransitionInput struct {
	To           Status `json:"to"`
	AgentComment string `json:"agentComment"`
	WorkLog      string `json:"workLog"`
	UserComment  string `json:"userComment"`
}
type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

var ErrNotFound = &Error{"not_found", 404, "resource not found"}

type Service struct{ DB *pgxpool.Pool }

func (s *Service) Create(ctx context.Context, tenant, project, principal, key string, a Actor, in CreateInput) (Ticket, error) {
	var out Ticket
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		requestHash := HashRequest(in)
		if _, lockErr := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, principal+":"+project+":"+key); lockErr != nil {
			return lockErr
		}
		var storedHash, storedResponse []byte
		lookupErr := tx.QueryRow(ctx, `SELECT request_hash,response FROM idempotency_keys WHERE project_id=$1 AND principal=$2 AND key=$3`, project, principal, key).Scan(&storedHash, &storedResponse)
		if lookupErr == nil {
			if !bytes.Equal(storedHash, requestHash[:]) {
				return &Error{"idempotency_conflict", 409, "Idempotency-Key reused with different payload"}
			}
			if err := json.Unmarshal(storedResponse, &out); err != nil {
				return err
			}
			return nil
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return lookupErr
		}
		if len(in.Text) < 1 || len(in.Text) > 4000 || in.Priority < 1 || in.Priority > 5 {
			return &Error{"invalid_request", 400, "invalid text or priority"}
		}
		if in.Page.URLPath == "" || in.Page.URLPath[0] != '/' {
			return &Error{"invalid_page", 400, "urlPath must be relative"}
		}
		var dueDate *time.Time
		if in.DueDate != nil {
			parsed, parseErr := time.Parse("2006-01-02", *in.DueDate)
			if parseErr != nil {
				return &Error{"invalid_due_date", 400, "dueDate must be YYYY-MM-DD"}
			}
			dueDate = &parsed
		}
		pageID := uuid.Must(uuid.NewV7())
		_, err := tx.Exec(ctx, `INSERT INTO pages(id,tenant_id,project_id,url_path,url_pattern,title) VALUES($1,$2,$3,$4,NULLIF($5,''),$6)`, pageID, tenant, project, in.Page.URLPath, in.Page.URLPattern, in.Page.Title)
		if err != nil {
			return conceal(err)
		}
		id := uuid.Must(uuid.NewV7())
		anchor, _ := json.Marshal(in.Anchor)
		selection, _ := json.Marshal(in.Selection)
		context, _ := json.Marshal(in.Context)
		err = tx.QueryRow(ctx, `INSERT INTO tickets(id,tenant_id,project_id,page_id,text,priority,due_date,anchor,selection,context,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')::uuid) RETURNING created_at,updated_at`, id, tenant, project, pageID, in.Text, in.Priority, dueDate, anchor, selection, context, a.ID).Scan(&out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			return conceal(err)
		}
		out = Ticket{ID: id.String(), TenantID: tenant, ProjectID: project, Text: in.Text, Priority: in.Priority, DueDate: dueDate, Status: New, Version: 1, Page: in.Page, Anchor: in.Anchor, Selection: in.Selection, Context: in.Context, CreatedAt: out.CreatedAt, UpdatedAt: out.UpdatedAt}
		if err := event(ctx, tx, tenant, project, out.ID, "created", a, "", New, map[string]any{"priority": in.Priority}); err != nil {
			return err
		}
		response, _ := json.Marshal(out)
		_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(tenant_id,project_id,principal,key,request_hash,status_code,response) VALUES($1,$2,$3,$4,$5,201,$6)`, tenant, project, principal, key, requestHash[:], response)
		return err
	})
	return out, err
}
func (s *Service) Get(ctx context.Context, tenant, project, id string) (Ticket, error) {
	var out Ticket
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return scanTicket(tx.QueryRow(ctx, `SELECT t.id,t.tenant_id,t.project_id,t.text,t.specification,t.priority,t.due_date,t.status,t.version,p.url_path,COALESCE(p.url_pattern,''),p.title,t.anchor,t.selection,t.context,t.created_at,t.updated_at FROM tickets t JOIN pages p ON (p.tenant_id,p.project_id,p.id)=(t.tenant_id,t.project_id,t.page_id) WHERE t.project_id=$1 AND t.id=$2`, project, id), &out)
	})
	return out, err
}
func (s *Service) List(ctx context.Context, tenant, project string, statuses []Status, limit int) ([]Ticket, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	out := []Ticket{}
	statusValues := make([]string, len(statuses))
	for index, status := range statuses {
		statusValues[index] = string(status)
	}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.id,t.tenant_id,t.project_id,t.text,t.specification,t.priority,t.due_date,t.status,t.version,p.url_path,COALESCE(p.url_pattern,''),p.title,t.anchor,t.selection,t.context,t.created_at,t.updated_at FROM tickets t JOIN pages p ON (p.tenant_id,p.project_id,p.id)=(t.tenant_id,t.project_id,t.page_id) WHERE t.project_id=$1 AND (cardinality($2::text[])=0 OR t.status::text=ANY($2::text[])) ORDER BY CASE WHEN t.status='rework' THEN 0 ELSE 1 END,t.priority DESC,t.due_date NULLS LAST,t.created_at LIMIT $3`, project, statusValues, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Ticket
			if err = scanTicket(rows, &t); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Service) ListFiltered(ctx context.Context, tenant, project string, statuses []Status, page string, limit int, cursor time.Time) ([]Ticket, *time.Time, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	statusValues := make([]string, len(statuses))
	for i, status := range statuses {
		statusValues[i] = string(status)
	}
	out := []Ticket{}
	var next *time.Time
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, ticketSelect+` WHERE t.project_id=$1 AND (cardinality($2::text[])=0 OR t.status::text=ANY($2::text[])) AND ($3='' OR p.url_path=$3 OR (p.url_pattern IS NOT NULL AND $3 LIKE replace(p.url_pattern,'*','%'))) AND ($4::timestamptz IS NULL OR t.updated_at<$4) ORDER BY t.updated_at DESC,t.id DESC LIMIT $5`, project, statusValues, page, func() any {
			if cursor.IsZero() {
				return nil
			}
			return cursor
		}(), limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Ticket
			if err = scanTicket(rows, &t); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if len(out) > limit {
		value := out[limit-1].UpdatedAt
		next = &value
		out = out[:limit]
	}
	return out, next, err
}
func (s *Service) Transition(ctx context.Context, tenant, project, id string, version int64, principal, key string, a Actor, in TransitionInput) (Ticket, error) {
	var out Ticket
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		var requestHash [32]byte
		if key != "" {
			requestHash = HashRequest(map[string]any{"ticketId": id, "version": version, "transition": in})
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, principal+":"+project+":"+key); err != nil {
				return err
			}
			var storedHash, storedResponse []byte
			lookup := tx.QueryRow(ctx, `SELECT request_hash,response FROM idempotency_keys WHERE project_id=$1 AND principal=$2 AND key=$3`, project, principal, key).Scan(&storedHash, &storedResponse)
			if lookup == nil {
				if !bytes.Equal(storedHash, requestHash[:]) {
					return &Error{"idempotency_conflict", 409, "Idempotency-Key reused with different payload"}
				}
				return json.Unmarshal(storedResponse, &out)
			}
			if !errors.Is(lookup, pgx.ErrNoRows) {
				return lookup
			}
		}
		if err := scanTicket(tx.QueryRow(ctx, `SELECT t.id,t.tenant_id,t.project_id,t.text,t.specification,t.priority,t.due_date,t.status,t.version,p.url_path,COALESCE(p.url_pattern,''),p.title,t.anchor,t.selection,t.context,t.created_at,t.updated_at FROM tickets t JOIN pages p ON (p.tenant_id,p.project_id,p.id)=(t.tenant_id,t.project_id,t.page_id) WHERE t.project_id=$1 AND t.id=$2 FOR UPDATE OF t`, project, id), &out); err != nil {
			return err
		}
		if out.Version != version {
			return &Error{"stale_version", 412, "ticket version changed"}
		}
		if err := allow(out.Status, in, a); err != nil {
			return err
		}
		if a.Kind == "agent" && in.To == Review {
			var owns bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_batch_tickets bt JOIN agent_batches b ON (b.tenant_id,b.project_id,b.id)=(bt.tenant_id,bt.project_id,bt.batch_id) WHERE bt.project_id=$1 AND bt.ticket_id=$2 AND bt.completed_at IS NULL AND b.active AND b.owner_subject=$3)`, project, id, a.ID).Scan(&owns); err != nil {
				return err
			}
			if !owns {
				return &Error{"batch_ownership_required", 403, "active batch ownership required"}
			}
		}
		meta := map[string]any{}
		if in.AgentComment != "" {
			meta["agentComment"] = in.AgentComment
		}
		if in.WorkLog != "" {
			meta["workLog"] = in.WorkLog
		}
		if in.UserComment != "" {
			meta["userComment"] = in.UserComment
		}
		prev := out.Status
		_, err := tx.Exec(ctx, `UPDATE tickets SET status=$1::ticket_status,previous_status=CASE WHEN $1::ticket_status='deleted' THEN status ELSE previous_status END,deleted_at=CASE WHEN $1::ticket_status='deleted' THEN now() ELSE NULL END,version=version+1,updated_at=now() WHERE tenant_id=$2 AND project_id=$3 AND id=$4`, in.To, tenant, project, id)
		if err != nil {
			return err
		}
		if err = event(ctx, tx, tenant, project, id, "transition", a, prev, in.To, meta); err != nil {
			return err
		}
		if a.Kind == "agent" && in.To == Review {
			if _, err = tx.Exec(ctx, `UPDATE agent_batch_tickets SET completed_at=now() WHERE project_id=$1 AND ticket_id=$2 AND completed_at IS NULL`, project, id); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE tickets SET claimed_batch_id=NULL WHERE tenant_id=$1 AND project_id=$2 AND id=$3`, tenant, project, id); err != nil {
				return err
			}
		}
		out.Status = in.To
		out.Version++
		out.UpdatedAt = time.Now()
		if key != "" {
			response, _ := json.Marshal(out)
			if _, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(tenant_id,project_id,principal,key,request_hash,status_code,response) VALUES($1,$2,$3,$4,$5,200,$6)`, tenant, project, principal, key, requestHash[:], response); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}
func (s *Service) Claim(ctx context.Context, tenant, project, principal, key string, ids []string, a Actor) (string, error) {
	if len(ids) < 1 || len(ids) > 50 {
		return "", &Error{"invalid_request", 400, "claim requires 1..50 tickets"}
	}
	if key == "" {
		return "", &Error{"idempotency_key_required", 400, "Idempotency-Key required"}
	}
	batch := uuid.Must(uuid.NewV7()).String()
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		requestHash := HashRequest(ids)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, principal+":"+project+":"+key); err != nil {
			return err
		}
		var storedHash, storedResponse []byte
		lookup := tx.QueryRow(ctx, `SELECT request_hash,response FROM idempotency_keys WHERE project_id=$1 AND principal=$2 AND key=$3`, project, principal, key).Scan(&storedHash, &storedResponse)
		if lookup == nil {
			if !bytes.Equal(storedHash, requestHash[:]) {
				return &Error{"idempotency_conflict", 409, "Idempotency-Key reused with different payload"}
			}
			var response map[string]string
			if err := json.Unmarshal(storedResponse, &response); err != nil {
				return err
			}
			batch = response["batchId"]
			return nil
		}
		if !errors.Is(lookup, pgx.ErrNoRows) {
			return lookup
		}
		rows, err := tx.Query(ctx, `SELECT id,status,claimed_batch_id FROM tickets WHERE project_id=$1 AND id=ANY($2::uuid[]) FOR UPDATE`, project, ids)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var id string
			var status Status
			var claimed *string
			if err = rows.Scan(&id, &status, &claimed); err != nil {
				rows.Close()
				return err
			}
			count++
			if (status != InProgress && status != Rework) || claimed != nil {
				rows.Close()
				return &Error{"claim_conflict", 409, "batch contains unavailable ticket"}
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if count != len(ids) {
			return ErrNotFound
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agent_batches(id,tenant_id,project_id,owner_subject) VALUES($1,$2,$3,$4)`, batch, tenant, project, a.ID); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err = tx.Exec(ctx, `INSERT INTO agent_batch_tickets(tenant_id,project_id,batch_id,ticket_id) VALUES($1,$2,$3,$4)`, tenant, project, batch, id); err != nil {
				return &Error{"claim_conflict", 409, "batch contains unavailable ticket"}
			}
			if _, err = tx.Exec(ctx, `UPDATE tickets SET claimed_batch_id=$1 WHERE tenant_id=$2 AND project_id=$3 AND id=$4`, batch, tenant, project, id); err != nil {
				return err
			}
		}
		response, _ := json.Marshal(map[string]string{"batchId": batch})
		_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(tenant_id,project_id,principal,key,request_hash,status_code,response) VALUES($1,$2,$3,$4,$5,201,$6)`, tenant, project, principal, key, requestHash[:], response)
		return err
	})
	return batch, err
}
func allow(from Status, in TransitionInput, a Actor) error {
	admin := a.Role == "admin" || a.Role == "owner"
	triage := admin || a.Role == "triager"
	agent := a.Kind == "agent" || a.Role == "agent"
	ok := false
	if triage && ((from == New && in.To == Clarification) || (from == Clarification && in.To == New) || ((from == New || from == Clarification) && in.To == InProgress)) {
		ok = true
	}
	if agent && (from == InProgress || from == Rework) && in.To == Review && in.AgentComment != "" && in.WorkLog != "" {
		ok = true
	}
	if admin && from == Review && in.To == Done {
		ok = true
	}
	if admin && from == Review && in.To == Rework && in.UserComment != "" {
		ok = true
	}
	if !ok {
		return &Error{"invalid_transition", 409, "transition is not allowed"}
	}
	return nil
}
func event(ctx context.Context, tx pgx.Tx, tenant, project, ticket, typ string, a Actor, from, to Status, meta map[string]any) error {
	b, _ := json.Marshal(meta)
	_, err := tx.Exec(ctx, `INSERT INTO ticket_events(tenant_id,project_id,ticket_id,type,actor_kind,actor_id,actor_name,from_status,to_status,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::ticket_status,NULLIF($9,'')::ticket_status,$10)`, tenant, project, ticket, typ, a.Kind, a.ID, a.Name, from, to, b)
	return err
}

type scanner interface{ Scan(...any) error }

func scanTicket(r scanner, t *Ticket) error {
	var anchor, selection, context []byte
	err := r.Scan(&t.ID, &t.TenantID, &t.ProjectID, &t.Text, &t.Specification, &t.Priority, &t.DueDate, &t.Status, &t.Version, &t.Page.URLPath, &t.Page.URLPattern, &t.Page.Title, &anchor, &selection, &context, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_ = json.Unmarshal(anchor, &t.Anchor)
	_ = json.Unmarshal(selection, &t.Selection)
	_ = json.Unmarshal(context, &t.Context)
	return nil
}
func HashRequest(v any) [32]byte { b, _ := json.Marshal(v); return sha256.Sum256(b) }
func ParseVersion(v string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(v, "\"%d\"", &n); err != nil || n < 1 {
		return 0, &Error{"if_match_required", 428, "valid If-Match is required"}
	}
	return n, nil
}
func conceal(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		return ErrNotFound
	}
	return err
}
