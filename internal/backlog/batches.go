package backlog

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/pikoolev/backlog-killer/internal/store"
	"time"
)

type BatchInfo struct {
	ID           string    `json:"id"`
	OwnerSubject string    `json:"ownerSubject"`
	TicketIDs    []string  `json:"ticketIds"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (s *Service) ListBatches(ctx context.Context, tenant, project string) ([]BatchInfo, error) {
	out := []BatchInfo{}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT b.id,b.owner_subject,array_agg(bt.ticket_id ORDER BY bt.ticket_id),b.active,b.created_at FROM agent_batches b JOIN agent_batch_tickets bt ON (bt.tenant_id,bt.project_id,bt.batch_id)=(b.tenant_id,b.project_id,b.id) WHERE b.project_id=$1 AND b.active AND bt.completed_at IS NULL GROUP BY b.id ORDER BY b.created_at`, project)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var item BatchInfo
			if e = rows.Scan(&item.ID, &item.OwnerSubject, &item.TicketIDs, &item.Active, &item.CreatedAt); e != nil {
				return e
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Service) UnassignBatch(ctx context.Context, tenant, project, batch string) error {
	return store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE agent_batches SET active=false,closed_at=now() WHERE project_id=$1 AND id=$2 AND active`, project, batch)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		if _, e = tx.Exec(ctx, `UPDATE agent_batch_tickets SET completed_at=now() WHERE project_id=$1 AND batch_id=$2 AND completed_at IS NULL`, project, batch); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE tickets SET claimed_batch_id=NULL WHERE project_id=$1 AND claimed_batch_id=$2`, project, batch)
		return e
	})
}
