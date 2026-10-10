package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Flows struct{ DB *pgxpool.Pool }
type EmbedStart struct{ ProjectKey, Origin, State, CodeChallenge string }
type EmbedCode struct{ Code, Origin, State string }

func (f *Flows) ValidateOrigin(ctx context.Context, projectKey, origin string) (tenantID, projectID string, err error) {
	parsed, e := url.Parse(origin)
	if e != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(parsed.Host, "*") {
		return "", "", errors.New("invalid origin")
	}
	err = f.DB.QueryRow(ctx, `SELECT tenant_id,project_id FROM lookup_project_origin($1,$2)`, projectKey, origin).Scan(&tenantID, &projectID)
	return
}
func (f *Flows) Start(ctx context.Context, input EmbedStart) (string, error) {
	if input.State == "" || input.CodeChallenge == "" {
		return "", errors.New("state and PKCE challenge required")
	}
	if _, _, err := f.ValidateOrigin(ctx, input.ProjectKey, input.Origin); err != nil {
		return "", errors.New("project origin not allowed")
	}
	stateHash := sha256.Sum256([]byte(input.State))
	id := randomToken(24)
	_, err := f.DB.Exec(ctx, `SELECT create_embed_flow($1::uuid,$2,$3,$4,$5,$6)`, tokenUUID(id), input.ProjectKey, input.Origin, stateHash[:], input.State, input.CodeChallenge)
	return id, err
}
func (f *Flows) IssueCode(ctx context.Context, flowID, subject string) (EmbedCode, error) {
	code := randomToken(32)
	hash := sha256.Sum256([]byte(code))
	var out EmbedCode
	err := f.DB.QueryRow(ctx, `SELECT origin,state_value FROM issue_embed_code($1::uuid,$2,$3)`, tokenUUID(flowID), subject, hash[:]).Scan(&out.Origin, &out.State)
	out.Code = code
	return out, err
}
func (f *Flows) Exchange(ctx context.Context, projectKey, origin, code, verifier string) (string, error) {
	codeHash := sha256.Sum256([]byte(code))
	var challenge, subject string
	err := f.DB.QueryRow(ctx, `SELECT code_challenge,subject FROM consume_embed_code($1,$2,$3)`, projectKey, origin, codeHash[:]).Scan(&challenge, &subject)
	if err != nil {
		return "", errors.New("invalid or expired authorization code")
	}
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		return "", errors.New("PKCE verification failed")
	}
	return subject, nil
}
func randomToken(size int) string {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func tokenUUID(value string) string {
	sum := sha256.Sum256([]byte(value))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return strings.ToLower(strings.Join([]string{formatHex(b[0:4]), formatHex(b[4:6]), formatHex(b[6:8]), formatHex(b[8:10]), formatHex(b[10:16])}, "-"))
}
func formatHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&15]
	}
	return string(out)
}
func InvitationHash(code string) []byte { sum := sha256.Sum256([]byte(code)); return sum[:] }
func InvitationExpiry() time.Time       { return time.Now().Add(7 * 24 * time.Hour) }
