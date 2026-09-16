package vote

import (
	"context"
	"fmt"

	"github.com/OpenSlides/openslides-go/datastore"
	"github.com/OpenSlides/openslides-go/datastore/cache"
	"github.com/OpenSlides/openslides-go/datastore/flow"
	"github.com/OpenSlides/openslides-go/environment"
)

// Flow initializes a cached connection to postgres.
func Flow(lookup environment.Environmenter, messageBus flow.Updater) (flow.Flow, func(context.Context) error, error) {
	postgres, init, err := datastore.NewFlowPostgres(lookup)
	if err != nil {
		return nil, nil, fmt.Errorf("init postgres: %w", err)
	}

	cache := cache.New(postgres)

	return cache, init, nil
}
