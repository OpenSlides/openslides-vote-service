// Package test impelemts a test suit to check if a backend implements all rules
// of the vote.Backend interface.
package test

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"sync"
	"testing"

	"github.com/OpenSlides/openslides-vote-service/internal/vote"
)

// Backend checks that a backend implements the vote.Backend interface.
func Backend(t *testing.T, backend vote.Backend) {
	t.Helper()

	pollID := 1
	t.Run("Start", func(t *testing.T) {
		t.Run("Start unknown poll", func(t *testing.T) {
			if err := backend.Start(context.Background(), pollID); err != nil {
				t.Errorf("Start an unknown poll returned error: %v", err)
			}
		})

		t.Run("Start started poll", func(t *testing.T) {
			backend.Start(context.Background(), pollID)
			if err := backend.Start(context.Background(), pollID); err != nil {
				t.Errorf("Start an started poll returned error: %v", err)
			}
		})

		t.Run("Start a stopped poll", func(t *testing.T) {
			if _, _, err := backend.Stop(context.Background(), pollID); err != nil {
				t.Fatalf("Stop returned: %v", err)
			}

			if err := backend.Start(context.Background(), pollID); err != nil {
				t.Errorf("Start an started poll returned error: %v", err)
			}

			err := backend.Vote(context.Background(), pollID, 5, []byte("my vote"))
			var errStopped interface{ Stopped() }
			if !errors.As(err, &errStopped) {
				t.Errorf("The stopped poll has to be stopped after calling start. Vote returned error: %v", err)
			}
		})
	})

	t.Run("Stop", func(t *testing.T) {
		t.Run("poll unknown", func(t *testing.T) {
			_, _, err := backend.Stop(context.Background(), 404)

			var errDoesNotExist interface{ DoesNotExist() }
			if !errors.As(err, &errDoesNotExist) {
				t.Fatalf("Stop a unknown poll has to return an error with a method DoesNotExist(), got: %v", err)
			}
		})
	})

	pollID++
	t.Run("Vote", func(t *testing.T) {
		t.Run("on notstarted poll", func(t *testing.T) {
			err := backend.Vote(context.Background(), pollID, 5, []byte("my vote"))

			var errDoesNotExist interface{ DoesNotExist() }
			if !errors.As(err, &errDoesNotExist) {
				t.Fatalf("Vote on a not started poll has to return an error with a method DoesNotExist(), got: %v", err)
			}
		})

		t.Run("successfull", func(t *testing.T) {
			backend.Start(context.Background(), pollID)

			if err := backend.Vote(context.Background(), pollID, 5, []byte("my vote")); err != nil {
				t.Fatalf("Vote returned unexpected error: %v", err)
			}

			data, userIDs, err := backend.Stop(context.Background(), pollID)
			if err != nil {
				t.Fatalf("Stop returned unexpected error: %v", err)
			}

			if len(data) != 1 {
				t.Fatalf("Found %d vote objects, expected 1", len(data))
			}

			if string(data[0]) != "my vote" {
				t.Errorf("Found vote object `%s`, expected `my vote`", data[0])
			}

			if len(userIDs) != 1 {
				t.Fatalf("Found %d user ids, expected 1", len(userIDs))
			}

			if userIDs[0] != 5 {
				t.Errorf("Got userID %d, expected 5", userIDs[0])
			}
		})

		pollID++
		t.Run("two times", func(t *testing.T) {
			backend.Start(context.Background(), pollID)

			if err := backend.Vote(context.Background(), pollID, 5, []byte("my vote")); err != nil {
				t.Fatalf("Vote returned unexpected error: %v", err)
			}

			err := backend.Vote(context.Background(), pollID, 5, []byte("my second vote"))

			if err == nil {
				t.Fatalf("Second vote did not return an error")
			}

			var errDoupleVote interface{ DoupleVote() }
			if !errors.As(err, &errDoupleVote) {
				t.Fatalf("Vote has to return a error with method DoupleVote. Got: %v", err)
			}
		})

		pollID++
		t.Run("on stopped vote", func(t *testing.T) {
			backend.Start(context.Background(), pollID)

			if _, _, err := backend.Stop(context.Background(), pollID); err != nil {
				t.Fatalf("Stop returned unexpected error: %v", err)
			}

			err := backend.Vote(context.Background(), pollID, 5, []byte("my vote"))

			if err == nil {
				t.Fatalf("Vote on stopped poll did not return an error")
			}

			var errStopped interface{ Stopped() }
			if !errors.As(err, &errStopped) {
				t.Fatalf("Vote has to return a error with method Stopped. Got: %v", err)
			}
		})
	})

	pollID++
	t.Run("Clear removes vote data", func(t *testing.T) {
		backend.Start(context.Background(), pollID)
		backend.Vote(context.Background(), pollID, 5, []byte("my vote"))

		if err := backend.Clear(context.Background(), pollID); err != nil {
			t.Fatalf("Clear returned unexpected error: %v", err)
		}

		bs, userIDs, err := backend.Stop(context.Background(), pollID)
		var errDoesNotExist interface{ DoesNotExist() }
		if !errors.As(err, &errDoesNotExist) {
			t.Fatalf("Stop a cleared poll has to return an error with a method DoesNotExist(), got: %v", err)
		}

		if len(bs) != 0 {
			t.Fatalf("Stop after clear returned unexpected data: %v", bs)
		}

		if len(userIDs) != 0 {
			t.Errorf("Stop after clear returned userIDs: %v", userIDs)
		}
	})

	pollID++
	t.Run("Clear removes voted users", func(t *testing.T) {
		backend.Start(context.Background(), pollID)
		backend.Vote(context.Background(), pollID, 5, []byte("my vote"))

		if err := backend.Clear(context.Background(), pollID); err != nil {
			t.Fatalf("Clear returned unexpected error: %v", err)
		}

		backend.Start(context.Background(), pollID)

		// Vote on the same poll with the same user id
		if err := backend.Vote(context.Background(), 6, 5, []byte("my vote")); err != nil {
			t.Fatalf("Vote after clear returned unexpected error: %v", err)
		}
	})

	pollID++
	t.Run("Concurrency", func(t *testing.T) {
		t.Run("Many Votes", func(t *testing.T) {
			count := 100
			backend.Start(context.Background(), pollID)

			var wg sync.WaitGroup
			for i := 0; i < count; i++ {
				wg.Add(1)
				go func(uid int) {
					defer wg.Done()

					if err := backend.Vote(context.Background(), pollID, uid, []byte("vote")); err != nil {
						t.Errorf("Vote %d returned undexpected error: %v", uid, err)
					}
				}(i + 1)
			}
			wg.Wait()

			data, userIDs, err := backend.Stop(context.Background(), pollID)
			if err != nil {
				t.Fatalf("Stop returned unexpected error: %v", err)
			}

			if len(data) != count {
				t.Fatalf("Found %d vote objects, expected %d", len(data), count)
			}

			if len(userIDs) != count {
				t.Fatalf("Found %d userIDs, expected %d", len(userIDs), count)
			}

			sort.Ints(userIDs)
			for i := 0; i < count; i++ {
				if userIDs[i] != i+1 {
					t.Fatalf("Found user id %d on place %d, expected %d", userIDs[i], i, i+1)
				}
			}
		})

		pollID++
		t.Run("Many starts and stops", func(t *testing.T) {
			starts := 50
			stops := 50

			var wg sync.WaitGroup
			for i := 0; i < starts; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					if err := backend.Start(context.Background(), pollID); err != nil {
						t.Errorf("Start returned undexpected error: %v", err)
					}
				}()
			}

			for i := 0; i < stops; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					if _, _, err := backend.Stop(context.Background(), pollID); err != nil {
						var errDoesNotExist interface{ DoesNotExist() }
						if errors.As(err, &errDoesNotExist) {
							// Does not exist errors are expected
							return
						}
						t.Errorf("Stop returned undexpected error: %v", err)
					}
				}()
			}
			wg.Wait()
		})

		pollID++
		t.Run("Many Stops and Votes", func(t *testing.T) {
			stopsCount := 50
			votesCount := 50

			backend.Start(context.Background(), pollID)

			expectedObjects := make([][][]byte, stopsCount)
			expectedUserIDs := make([][]int, stopsCount)
			var stoppedErrsMu sync.Mutex
			var stoppedErrs int

			var wg sync.WaitGroup
			for i := 0; i < votesCount; i++ {
				wg.Add(1)
				go func(uid int) {
					defer wg.Done()

					err := backend.Vote(context.Background(), pollID, uid, []byte("vote"))

					if err != nil {
						var errStopped interface{ Stopped() }
						if errors.As(err, &errStopped) {
							// Stopped errors are expected.
							stoppedErrsMu.Lock()
							stoppedErrs++
							stoppedErrsMu.Unlock()
							return
						}

						t.Errorf("Vote %d returned undexpected error: %v", uid, err)
					}

				}(i + 1)
			}

			// Let the other goroutines run.
			runtime.Gosched()

			for i := 0; i < stopsCount; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()

					obj, userIDs, err := backend.Stop(context.Background(), pollID)

					if err != nil {
						t.Errorf("Stop returned undexpected error: %v", err)
						return
					}
					expectedObjects[i] = obj
					expectedUserIDs[i] = userIDs
				}(i)
			}
			wg.Wait()

			expectedVotes := votesCount - stoppedErrs

			for _, objs := range expectedObjects {
				if len(objs) != expectedVotes {
					t.Errorf("Stop returned %d objects, expected %d: %v", len(objs), expectedVotes, objs)
				}
			}

			for _, userIDs := range expectedUserIDs {
				if len(userIDs) != expectedVotes {
					t.Errorf("Stop returned %d userIDs, expected %d", len(userIDs), expectedVotes)
				}
			}
		})
	})
}
