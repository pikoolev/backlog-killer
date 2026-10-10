package projects

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pikoolev/backlog-killer/internal/store"
)

type Tokens struct{ DB *pgxpool.Pool }
type TokenSecret struct {
	ID        string     `json:"id"`
	Secret    string     `json:"secret"`
	Prefix    string     `json:"prefix"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt"`
}
type TokenInfo struct {
	ID         string     `json:"id"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

var allowedScopes = map[string]bool{"tickets:read": true, "tickets:claim": true, "tickets:comment": true, "tickets:complete": true}

func (s *Tokens) Create(ctx context.Context, tenant, project, creator string, scopes []string, expires *time.Time) (TokenSecret, error) {
	if len(scopes) == 0 {
		return TokenSecret{}, errors.New("at least one scope required")
	}
	for _, scope := range scopes {
		if !allowedScopes[scope] {
			return TokenSecret{}, errors.New("unsupported scope")
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return TokenSecret{}, err
	}
	prefixBytes := make([]byte, 6)
	_, _ = rand.Read(prefixBytes)
	prefix := strings.ToLower(base64.RawURLEncoding.EncodeToString(prefixBytes))
	secret := "bkk_pat_" + prefix + "_" + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(secret))
	out := TokenSecret{ID: uuid.Must(uuid.NewV7()).String(), Secret: secret, Prefix: prefix, Scopes: scopes, ExpiresAt: expires}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_tokens(id,tenant_id,project_id,prefix,secret_hash,scopes,created_by,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, out.ID, tenant, project, prefix, hash[:], scopes, creator, expires)
		return err
	})
	return out, err
}
func (s *Tokens) List(ctx context.Context, tenant, project string) ([]TokenInfo, error) {
	out := []TokenInfo{}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,prefix,scopes,expires_at,revoked_at,last_used_at FROM agent_tokens WHERE project_id=$1 ORDER BY created_at DESC`, project)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item TokenInfo
			if err = rows.Scan(&item.ID, &item.Prefix, &item.Scopes, &item.ExpiresAt, &item.RevokedAt, &item.LastUsedAt); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Tokens) Revoke(ctx context.Context, tenant, project, id string) error {
	return store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE agent_tokens SET revoked_at=now() WHERE project_id=$1 AND id=$2 AND revoked_at IS NULL`, project, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("token not found")
		}
		return nil
	})
}
