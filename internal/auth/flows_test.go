package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmbedFlowOneTimePKCE(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	flows := Flows{DB: db}
	verifier := "deterministic-test-verifier-with-sufficient-length"
	sum := sha256.Sum256([]byte(verifier))
	flow, err := flows.Start(ctx, EmbedStart{ProjectKey: "demo-project", Origin: "http://127.0.0.1:4174", State: "state", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := flows.IssueCode(ctx, flow, "30000000-0000-4000-8000-000000000003")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := flows.Exchange(ctx, "demo-project", issued.Origin, issued.Code, verifier)
	if err != nil || subject != "30000000-0000-4000-8000-000000000003" {
		t.Fatalf("subject=%s err=%v", subject, err)
	}
	if _, err = flows.Exchange(ctx, "demo-project", issued.Origin, issued.Code, verifier); err == nil {
		t.Fatal("authorization code replay succeeded")
	}
}
