package vote

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/OpenSlides/openslides-autoupdate-service/pkg/datastore"
	"github.com/OpenSlides/openslides-autoupdate-service/pkg/dsmock"
)

func TestPreload(t *testing.T) {
	for _, tt := range []struct {
		name        string
		data        string
		expectCount int
	}{
		{
			"one user",
			`---
			meeting/5/id: 5
			poll/1:
				meeting_id: 5
				entitled_group_ids: [30]
				pollmethod: Y
				global_yes: true

			group/30/user_ids: [50]

			user:
				50:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [31]
					is_present_in_meeting: [5]
			`,
			2,
		},

		{
			"Many groups",
			`---
			meeting/5/id: 5
			poll/1:
				meeting_id: 5
				entitled_group_ids: [30,31]
				pollmethod: Y
				global_yes: true

			group/30/user_ids: [50]
			group/31/user_ids: [50]

			user:
				50:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [30]
					is_present_in_meeting: [5]
			`,
			2,
		},

		{
			"Many users",
			`---
			meeting/5/id: 5
			poll/1:
				meeting_id: 5
				entitled_group_ids: [30]
				pollmethod: Y
				global_yes: true

			group/30/user_ids: [50,51]

			user:
				50:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [30]
					is_present_in_meeting: [5]

				51:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [30]
					is_present_in_meeting: [5]
			`,
			2,
		},

		{
			"Many users in different groups",
			`---
			meeting/5/id: 5
			poll/1:
				meeting_id: 5
				entitled_group_ids: [30, 31]
				pollmethod: Y
				global_yes: true

			group/30/user_ids: [50]
			group/31/user_ids: [51]

			user:
				50:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [30]
					is_present_in_meeting: [5]

				51:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [30]
					is_present_in_meeting: [5]
			`,
			2,
		},

		{
			"Many users in different groups with delegation",
			`---
			meeting/5/id: 5
			poll/1:
				meeting_id: 5
				entitled_group_ids: [30, 31]
				pollmethod: Y
				global_yes: true

			group/30/user_ids: [50]
			group/31/user_ids: [51]

			user:
				50:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [30]
					is_present_in_meeting: [5]
					vote_delegated_$5_to_id: 52

				51:
					is_present_in_meeting_ids: [5]
					group_$5_ids: [30]
					is_present_in_meeting: [5]
					vote_delegated_$5_to_id: 53

				52:
					is_present_in_meeting_ids: [5]

				53:
					is_present_in_meeting_ids: [5]
			`,
			3,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dsCount := NewRequestCounter(dsmock.Stub(dsmock.YAMLData(tt.data)))
			ds := NewRequestCache(dsCount)

			poll, err := loadPoll(context.Background(), datastore.NewRequest(ds), 1)
			if err != nil {
				t.Fatalf("loadPoll returned: %v", err)
			}

			dsCount.Reset()
			poll.preload(context.Background(), datastore.NewRequest(ds))

			if err != nil {
				t.Errorf("preload returned: %v", err)
			}

			if got := dsCount.Value(); got != tt.expectCount {
				buf := new(bytes.Buffer)
				for _, req := range dsCount.Requests() {
					fmt.Fprintln(buf, req)
				}
				t.Errorf("preload send %d requests, expected %d:\n%s", got, tt.expectCount, buf)
			}
		})
	}
}

type RequestCounter struct {
	mu sync.Mutex

	ds       datastore.Getter
	requests [][]string
}

func NewRequestCounter(ds datastore.Getter) *RequestCounter {
	return &RequestCounter{ds: ds}
}

func (ds *RequestCounter) Get(ctx context.Context, keys ...string) (map[string][]byte, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.requests = append(ds.requests, keys)
	return ds.ds.Get(ctx, keys...)
}

func (ds *RequestCounter) Reset() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.requests = nil
}

func (ds *RequestCounter) Value() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	return len(ds.requests)
}

func (ds *RequestCounter) Requests() [][]string {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	return ds.requests
}

type RequestCache struct {
	mu sync.Mutex

	ds    datastore.Getter
	cache map[string][]byte
}

func NewRequestCache(ds datastore.Getter) *RequestCache {
	return &RequestCache{ds: ds, cache: make(map[string][]byte)}
}

func (ds *RequestCache) Get(ctx context.Context, keys ...string) (map[string][]byte, error) {
	if len(keys) == 0 {
		// TODO: This has to be fixed in datastore.Request
		return nil, nil
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	out := make(map[string][]byte, len(keys))
	var needKeys []string
	for _, key := range keys {
		v, ok := ds.cache[key]
		if !ok {
			needKeys = append(needKeys, key)
			continue
		}
		out[key] = v
	}

	if len(needKeys) == 0 {
		return out, nil
	}

	upstream, err := ds.ds.Get(ctx, needKeys...)
	if err != nil {
		return nil, fmt.Errorf("upstream: %w", err)
	}

	for k, v := range upstream {
		out[k] = v
		ds.cache[k] = v
	}
	return out, nil
}
