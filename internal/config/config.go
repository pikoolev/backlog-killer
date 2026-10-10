package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Mode, DatabaseURL, MigrationDatabaseURL, OIDCIssuer, OIDCPublicIssuer, OIDCAudience                      string
	OIDCClientID, OIDCClientSecret                                                                           string
	APIPublicURL, WebPublicURL, EmbedSigningKey, StorageDriver, StoragePath                                  string
	MCPAllowedOrigins                                                                                        []string
	LocalDemoSeed                                                                                            bool
	OnPremTenant, OnPremProject, OnPremProjectKey, OnPremOwnerSubject, OnPremOwnerEmail, OnPremProjectOrigin string
}

func Load() (Config, error) {
	c := Config{
		Mode: env("DEPLOYMENT_MODE", "saas"), DatabaseURL: os.Getenv("DATABASE_URL"), MigrationDatabaseURL: os.Getenv("MIGRATION_DATABASE_URL"),
		OIDCIssuer: os.Getenv("OIDC_ISSUER"), OIDCPublicIssuer: os.Getenv("OIDC_PUBLIC_ISSUER"), OIDCAudience: os.Getenv("OIDC_AUDIENCE"), OIDCClientID: env("OIDC_CLIENT_ID", "backlog-killer-web"), OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		APIPublicURL: os.Getenv("API_PUBLIC_URL"), WebPublicURL: os.Getenv("WEB_PUBLIC_URL"), EmbedSigningKey: os.Getenv("EMBED_SIGNING_KEY"),
		StorageDriver: env("STORAGE_DRIVER", "filesystem"), StoragePath: os.Getenv("STORAGE_PATH"), MCPAllowedOrigins: split(os.Getenv("MCP_ALLOWED_ORIGINS")),
	}
	c.OnPremTenant, c.OnPremProject, c.OnPremProjectKey = os.Getenv("ONPREM_TENANT_NAME"), os.Getenv("ONPREM_PROJECT_NAME"), os.Getenv("ONPREM_PROJECT_KEY")
	c.OnPremOwnerSubject, c.OnPremOwnerEmail, c.OnPremProjectOrigin = os.Getenv("ONPREM_OWNER_SUBJECT"), os.Getenv("ONPREM_OWNER_EMAIL"), os.Getenv("ONPREM_PROJECT_ORIGIN")
	seed := os.Getenv("LOCAL_DEMO_SEED")
	if seed != "" && seed != "true" && seed != "false" {
		return c, errors.New("LOCAL_DEMO_SEED must be true or false")
	}
	c.LocalDemoSeed = seed == "true"
	if c.Mode != "saas" && c.Mode != "onprem" {
		return c, errors.New("DEPLOYMENT_MODE must be saas or onprem")
	}
	for name, value := range map[string]string{"DATABASE_URL": c.DatabaseURL, "MIGRATION_DATABASE_URL": c.MigrationDatabaseURL, "OIDC_ISSUER": c.OIDCIssuer, "OIDC_PUBLIC_ISSUER": c.OIDCPublicIssuer, "OIDC_AUDIENCE": c.OIDCAudience, "API_PUBLIC_URL": c.APIPublicURL, "WEB_PUBLIC_URL": c.WebPublicURL, "EMBED_SIGNING_KEY": c.EmbedSigningKey, "STORAGE_PATH": c.StoragePath} {
		if value == "" {
			return c, fmt.Errorf("%s is required", name)
		}
	}
	if len(c.EmbedSigningKey) < 32 {
		return c, errors.New("EMBED_SIGNING_KEY must be at least 32 bytes")
	}
	if c.Mode == "onprem" {
		for name, value := range map[string]string{"ONPREM_TENANT_NAME": c.OnPremTenant, "ONPREM_PROJECT_NAME": c.OnPremProject, "ONPREM_PROJECT_KEY": c.OnPremProjectKey, "ONPREM_OWNER_SUBJECT": c.OnPremOwnerSubject, "ONPREM_OWNER_EMAIL": c.OnPremOwnerEmail, "ONPREM_PROJECT_ORIGIN": c.OnPremProjectOrigin} {
			if strings.TrimSpace(value) == "" {
				return c, fmt.Errorf("%s is required in onprem mode", name)
			}
		}
		if err := exactOrigin(c.OnPremProjectOrigin); err != nil {
			return c, fmt.Errorf("ONPREM_PROJECT_ORIGIN: %w", err)
		}
	}
	for _, raw := range append([]string{c.APIPublicURL, c.WebPublicURL}, c.MCPAllowedOrigins...) {
		if err := exactOrigin(raw); err != nil {
			return c, err
		}
	}
	for name, raw := range map[string]string{"OIDC_ISSUER": c.OIDCIssuer, "OIDC_PUBLIC_ISSUER": c.OIDCPublicIssuer} {
		issuer, err := url.Parse(raw)
		if err != nil || (issuer.Scheme != "http" && issuer.Scheme != "https") || issuer.Host == "" || issuer.RawQuery != "" || issuer.Fragment != "" || strings.Contains(issuer.Host, "*") {
			return c, fmt.Errorf("invalid %s %q", name, raw)
		}
	}
	if c.StorageDriver != "filesystem" {
		return c, errors.New("STORAGE_DRIVER must be filesystem")
	}
	return c, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func split(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func exactOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Host, "*") {
		return fmt.Errorf("invalid exact origin %q", raw)
	}
	return nil
}
