package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/OpenSlides/openslides-vote-service/backend/postgres"
	"github.com/OpenSlides/openslides-vote-service/backend/test"
	"github.com/ory/dockertest/v3"
)

func startPostgres(t *testing.T) (string, func()) {
	t.Helper()

	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("Could not connect to docker: %s", err)
	}

	runOpts := dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "13",
		Env: []string{
			"POSTGRES_USER=postgres",
			"POSTGRES_PASSWORD=password",
			"POSTGRES_DB=database",
		},
	}

	resource, err := pool.RunWithOptions(&runOpts)
	if err != nil {
		t.Fatalf("Could not start postgres container: %s", err)
	}

	return resource.GetPort("5432/tcp"), func() {
		if err = pool.Purge(resource); err != nil {
			t.Fatalf("Could not purge postgres container: %s", err)
		}
	}
}

func TestImplementBackendInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("Skip Postgres Test")
	}

	ctx := context.Background()
	port, close := startPostgres(t)
	defer close()

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
