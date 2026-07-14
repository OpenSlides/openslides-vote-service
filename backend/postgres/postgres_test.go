package postgres_test

import (
	"fmt"
	"testing"

	"github.com/OpenSlides/openslides-vote-service/backend/postgres"
	"github.com/OpenSlides/openslides-vote-service/backend/test"
	"github.com/ory/dockertest/v4"
)

func startPostgres(t *testing.T) string {
	t.Helper()

	pool := dockertest.NewPoolT(t, "")
	postgres := pool.RunT(t, "postgres",
		dockertest.WithTag("17"),
		dockertest.WithEnv([]string{
			"POSTGRES_USER=postgres",
			"POSTGRES_PASSWORD=password",
			"POSTGRES_DB=database",
		}),
	)

	return postgres.GetPort("5432/tcp")
}

func TestImplementBackendInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("Skip Postgres Test")
	}

	ctx := t.Context()
	port := startPostgres(t)

	addr := fmt.Sprintf(`user=postgres password='password' host=localhost port=%s dbname=database`, port)
	p, err := postgres.New(ctx, addr)
	if err != nil {
		t.Fatalf("Creating postgres backend returned: %v", err)
	}
	defer p.Close()

	p.Wait(ctx)
	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("Creating db schema: %v", err)
	}

	t.Logf("Postgres port: %s", port)

	test.Backend(t, p)
}
