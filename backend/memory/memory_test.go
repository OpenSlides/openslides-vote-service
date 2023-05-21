package memory_test

import (
	"testing"

	"github.com/OpenSlides/openslides-vote-service/backend/memory"
	"github.com/OpenSlides/openslides-vote-service/backend/test"
)

func TestBackend(t *testing.T) {
	m := memory.New()

	test.Backend(t, m)
}
