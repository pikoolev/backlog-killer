package config

import "testing"

func TestExactOrigin(t *testing.T) {
	for _, v := range []string{"*", "https://example.test/path", "javascript:alert(1)", "https://*.example.test"} {
		if exactOrigin(v) == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	if err := exactOrigin("https://example.test:8443"); err != nil {
		t.Fatal(err)
	}
}

func validEnvironment(t *testing.T) {
	t.Helper()
	values := map[string]string{"DEPLOYMENT_MODE": "saas", "DATABASE_URL": "postgres://runtime/db", "MIGRATION_DATABASE_URL": "postgres://migrator/db", "OIDC_ISSUER": "https://id.example/realm", "OIDC_PUBLIC_ISSUER": "https://id.example/realm", "OIDC_AUDIENCE": "api", "API_PUBLIC_URL": "https://api.example", "WEB_PUBLIC_URL": "https://web.example", "EMBED_SIGNING_KEY": "0123456789abcdef0123456789abcdef", "STORAGE_DRIVER": "filesystem", "STORAGE_PATH": t.TempDir()}
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func TestLoadRejectsUnsupportedStorage(t *testing.T) {
	validEnvironment(t)
	t.Setenv("STORAGE_DRIVER", "s3")
	if _, err := Load(); err == nil {
		t.Fatal("accepted unimplemented storage backend")
	}
}

func TestProductionRejectsDemoSeedAndHTTP(t *testing.T) {
	validEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOCAL_DEMO_SEED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("production accepted demo seed")
	}
	validEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOCAL_DEMO_SEED", "false")
	t.Setenv("API_PUBLIC_URL", "http://api.example")
	if _, err := Load(); err == nil {
		t.Fatal("production accepted HTTP public URLs")
	}
}

func TestOnPremRequiresBootstrapIdentity(t *testing.T) {
	validEnvironment(t)
	t.Setenv("DEPLOYMENT_MODE", "onprem")
	if _, err := Load(); err == nil {
		t.Fatal("accepted on-prem mode without bootstrap identity")
	}
}

func TestSaaSProfileLoadsWithoutOnPremInputs(t *testing.T) {
	validEnvironment(t)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
