package redis_test

import (
	"context"
	"testing"

	"github.com/OpenSlides/openslides-vote-service/backend/redis"
	"github.com/OpenSlides/openslides-vote-service/backend/test"
	"github.com/ory/dockertest/v4"
)

func startRedis(t *testing.T) string {
	t.Helper()

	pool := dockertest.NewPoolT(t, "")

	redis := pool.RunT(t, "redis",
		dockertest.WithTag("6.2"),
	)

	return redis.GetPort("6379/tcp")
}

func TestImplementBackendInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("Skip Redis Test")
	}

	port := startRedis(t)

	r := redis.New("localhost:" + port)
	r.Wait(context.Background())
	t.Logf("Redis port: %s", port)

	test.Backend(t, r)
}
