package auth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Principal struct {
	Kind, ID, Name, Issuer, Subject, Email, TenantID, ProjectID, Role string
	EmailVerified                                                     bool
	Scopes                                                            map[string]bool
}
type contextKey struct{}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, principal)
}

type Authenticator struct {
	DB                                        *pgxpool.Pool
	Issuer, PublicIssuer, APIIssuer, Audience string
	EmbedKey                                  []byte
	client                                    *http.Client
	mu                                        sync.Mutex
	keys                                      map[string]*rsa.PublicKey
	expires                                   time.Time
}

func New(db *pgxpool.Pool, issuer, publicIssuer, apiIssuer, audience, embedKey string) *Authenticator {
	return &Authenticator{DB: db, Issuer: issuer, PublicIssuer: publicIssuer, APIIssuer: apiIssuer, Audience: audience, EmbedKey: []byte(embedKey), client: &http.Client{Timeout: 5 * time.Second}}
}
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		p, err := a.Authenticate(r.Context(), raw)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, p)))
	})
}
func (a *Authenticator) Authenticate(ctx context.Context, raw string) (Principal, error) {
	if strings.HasPrefix(raw, "bkk_pat_") {
		return a.pat(ctx, raw)
	}
	if token, err := a.embed(raw); err == nil {
		return token, nil
	}
	return a.oidc(ctx, raw)
}
func patPrefix(raw string) (string, error) {
	remainder, ok := strings.CutPrefix(raw, "bkk_pat_")
	if !ok {
		return "", errors.New("invalid PAT")
	}
	prefix, secret, ok := strings.Cut(remainder, "_")
	if !ok || prefix == "" || secret == "" {
		return "", errors.New("invalid PAT")
	}
	return prefix, nil
}
func (a *Authenticator) pat(ctx context.Context, raw string) (Principal, error) {
	prefix, err := patPrefix(raw)
	if err != nil {
		return Principal{}, err
	}
	sum := sha256.Sum256([]byte(raw))
	var p Principal
	var hash []byte
	var scopes []string
	var expires, revoked *time.Time
	err = a.DB.QueryRow(ctx, `SELECT id,tenant_id,project_id,prefix,secret_hash,scopes,expires_at,revoked_at FROM authenticate_agent_token($1)`, prefix).Scan(&p.ID, &p.TenantID, &p.ProjectID, &p.Name, &hash, &scopes, &expires, &revoked)
	if err != nil || len(hash) != len(sum) || subtle.ConstantTimeCompare(hash, sum[:]) != 1 || revoked != nil || (expires != nil && expires.Before(time.Now())) {
		return Principal{}, errors.New("invalid PAT")
	}
	p.Kind = "agent"
	p.Role = "agent"
	p.Scopes = map[string]bool{}
	for _, s := range scopes {
		p.Scopes[s] = true
	}
	_, _ = a.DB.Exec(ctx, `UPDATE agent_tokens SET last_used_at=CASE WHEN last_used_at IS NULL OR last_used_at < now()-interval '5 minutes' THEN now() ELSE last_used_at END WHERE id=$1`, p.ID)
	return p, nil
}

func (a *Authenticator) embed(raw string) (Principal, error) {
	claims := EmbedClaims{}
	token, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "HS256" {
			return nil, errors.New("unexpected embed algorithm")
		}
		return a.EmbedKey, nil
	}, jwt.WithAudience(a.Audience), jwt.WithIssuer(a.APIIssuer), jwt.WithExpirationRequired(), jwt.WithLeeway(10*time.Second))
	if err != nil || !token.Valid {
		return Principal{}, errors.New("invalid embed token")
	}
	return Principal{Kind: "user", ID: claims.Subject, Subject: claims.Subject, TenantID: claims.TenantID, ProjectID: claims.ProjectID, Role: claims.Role, Scopes: map[string]bool{}}, nil
}

type claims struct {
	jwt.RegisteredClaims
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
}

func (a *Authenticator) oidc(ctx context.Context, raw string) (Principal, error) {
	c := claims{}
	token, err := jwt.ParseWithClaims(raw, &c, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "RS256" {
			return nil, errors.New("unexpected signing algorithm")
		}
		kid, _ := t.Header["kid"].(string)
		return a.key(ctx, kid)
	}, jwt.WithAudience(a.Audience), jwt.WithIssuer(a.PublicIssuer), jwt.WithExpirationRequired(), jwt.WithLeeway(30*time.Second))
	if err != nil || !token.Valid {
		return Principal{}, errors.New("invalid OIDC token")
	}
	return Principal{Kind: "user", ID: c.Subject, Name: c.PreferredUsername, Issuer: c.Issuer, Subject: c.Subject, Email: c.Email, EmailVerified: c.EmailVerified, Scopes: map[string]bool{}}, nil
}
func (a *Authenticator) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Now().Before(a.expires) && a.keys[kid] != nil {
		return a.keys[kid], nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.Issuer+"/protocol/openid-connect/certs", nil)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("jwks status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []struct{ Kid, Kty, N, E string }
	}
	if err = json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	a.keys = map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 + int(b)
		}
		a.keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
	}
	a.expires = time.Now().Add(time.Hour)
	if a.keys[kid] == nil {
		return nil, errors.New("unknown signing key")
	}
	return a.keys[kid], nil
}
