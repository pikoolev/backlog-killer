package projects

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pikoolev/backlog-killer/internal/store"
)

type Invitations struct{ DB *pgxpool.Pool }
type Invitation struct {
	ID, URL   string
	ExpiresAt time.Time
}

func (s *Invitations) Create(ctx context.Context, tenant, project, email, role, publicURL string) (Invitation, error) {
	if email == "" || (role != "admin" && role != "triager" && role != "reporter") {
		return Invitation{}, errors.New("invalid invitation")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Invitation{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(code))
	out := Invitation{ID: uuid.Must(uuid.NewV7()).String(), URL: publicURL + "/invitations/accept?code=" + code, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO invitations(id,tenant_id,project_id,token_hash,email,role,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, out.ID, tenant, project, hash[:], email, role, out.ExpiresAt)
		return err
	})
	return out, err
}
func (s *Invitations) Accept(ctx context.Context, code, issuer, subject, email string) (tenant, project, role string, err error) {
	hash := sha256.Sum256([]byte(code))
	err = s.DB.QueryRow(ctx, `SELECT tenant_id,project_id,role FROM accept_invitation($1,$2,$3,$4)`, hash[:], issuer, subject, email).Scan(&tenant, &project, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		err = errors.New("invalid, expired, used, or email-mismatched invitation")
	}
	return
}
