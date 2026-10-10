package backlog

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const demoTenant = "33333333-3333-4333-8333-333333333333"
const demoProject = "33333333-3333-4333-8333-333333333334"

func integrationService(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Service{DB: pool}
}
func TestMissingTenantContextRevealsNothing(t *testing.T) {
	service := integrationService(t)
	var count int
	if err := service.DB.QueryRow(context.Background(), `SELECT count(*) FROM tickets`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("missing RLS context exposed %d tickets", count)
	}
}
func TestCrossProjectGetConcealsTicket(t *testing.T) {
	service := integrationService(t)
	ticket, err := service.Create(context.Background(), demoTenant, demoProject, "test:cross", uuid.NewString(), Actor{Kind: "user", ID: "30000000-0000-4000-8000-000000000002", Role: "triager"}, CreateInput{Text: "cross project", Priority: 3, Page: Page{URLPath: "/", Title: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Get(context.Background(), demoTenant, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ticket.ID)
	if err != ErrNotFound {
		t.Fatalf("cross-project error=%v", err)
	}
}
func TestDualClaimHasOneWinner(t *testing.T) {
	service := integrationService(t)
	ticket, err := service.Create(context.Background(), demoTenant, demoProject, "test:agent", uuid.NewString(), Actor{Kind: "user", ID: "30000000-0000-4000-8000-000000000002", Name: "triager", Role: "triager"}, CreateInput{Text: "dual claim", Priority: 5, Page: Page{URLPath: "/", Title: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	ticket, err = service.Transition(context.Background(), demoTenant, demoProject, ticket.ID, 1, Actor{Kind: "user", ID: "30000000-0000-4000-8000-000000000002", Role: "triager"}, TransitionInput{To: InProgress})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			_, claimErr := service.Claim(context.Background(), demoTenant, demoProject, []string{ticket.ID}, Actor{Kind: "agent", ID: string(rune('a' + id)), Role: "agent"})
			results <- claimErr
		}(index)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	var failures []error
	for claimErr := range results {
		if claimErr == nil {
			success++
		} else {
			failures = append(failures, claimErr)
		}
	}
	if success != 1 {
		t.Fatalf("claim winners=%d failures=%v", success, failures)
	}
}
