package vote

import (
	"context"
	"fmt"

	"github.com/OpenSlides/openslides-go/datastore"
	"github.com/OpenSlides/openslides-go/datastore/cache"
	"github.com/OpenSlides/openslides-go/datastore/flow"
	"github.com/OpenSlides/openslides-go/environment"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Flow initializes a cached connection to postgres.
func Flow(lookup environment.Environmenter) (flow.Flow, func(context.Context) error, *pgxpool.Pool, error) {
	postgres, init, err := datastore.NewFlowPostgres(lookup)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init postgres: %w", err)
	}

	pool := postgres.Pool

	cache := cache.New(postgres)

	return cache, init, pool, nil
}
