package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type EmbedIssuer struct {
	Key              []byte
	Issuer, Audience string
}
type EmbedClaims struct {
	jwt.RegisteredClaims
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
	Role      string `json:"role"`
}

func (i EmbedIssuer) Issue(subject, tenant, project, role string) (string, error) {
	if len(i.Key) < 32 {
		return "", errors.New("embed signing key too short")
	}
	now := time.Now()
	claims := EmbedClaims{RegisteredClaims: jwt.RegisteredClaims{Issuer: i.Issuer, Subject: subject, Audience: jwt.ClaimStrings{i.Audience}, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute))}, TenantID: tenant, ProjectID: project, Role: role}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.Key)
}
