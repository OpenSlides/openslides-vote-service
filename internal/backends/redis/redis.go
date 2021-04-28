package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/OpenSlides/openslides-vote-service/internal/log"
	"github.com/gomodule/redigo/redis"
)

const (
	keyState = "vote_state_%d"
	keyVote  = "vote_data_%d"
)

// Backend is a vote-Backend.
//
// Is tries to save the votes as fast as possible. All necessary checkes are
// done inside a lua-script so everything is done in one atomic step. It is
// expected that there is no backup from the redis database. Everyone with
// access to the redis database can see the vote results and how each user has
// voted.
//
// Has to be created with redis.New().
type Backend struct {
	pool *redis.Pool
}

// New creates an initializes Redis instance.
func New(addr string) *Backend {
	pool := redis.Pool{
		MaxActive:   100,
		Wait:        true,
		MaxIdle:     10,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
	return &Backend{
		pool: &pool,
	}
}

// Wait blocks until a connection to redis can be established.
func (b *Backend) Wait(ctx context.Context) {
	for ctx.Err() == nil {
		conn := b.pool.Get()
		_, err := conn.Do("PING")
		conn.Close()
		if err == nil {
			return
		}
		log.Info("Waiting for redis: %v", err)
		time.Sleep(500 * time.Millisecond)
	}
}

func (b *Backend) String() string {
	return "redis"
}

// Start starts the poll.
func (b *Backend) Start(ctx context.Context, pollID int) error {
	conn := b.pool.Get()
	defer conn.Close()

	sKey := fmt.Sprintf(keyState, pollID)

	log.Debug("Redis: SETNX %s 1", sKey)
	if _, err := conn.Do("SETNX", sKey, 1); err != nil {
		return fmt.Errorf("set state key to 1: %w", err)
	}
	return nil
}

// luaVoteScript checks for condition and saves a vote if all checks pass.
//
// KEYS[1] == state key
// KEYS[2] == vote data
// ARGV[1] == userID
// ARGV[2] == Vote object
//
// Returns 0 on success.
// Returns 1 if the poll is not started.
// Returns 2 if the poll was stopped.
// Returns 3 if the user has already voted.
const luaVoteScript = `
local state = redis.call("GET",KEYS[1])
if state == false then 
	return 1
end

if state == "2" then
	return 2
end

local saved = redis.call("HSETNX",KEYS[2],ARGV[1],ARGV[2])
if saved == 0 then
	return 3
end

return 0`

// Vote saves a vote in redis.
//
// It also checks, that the user did not vote before and that the poll is open.
func (b *Backend) Vote(ctx context.Context, pollID int, userID int, object []byte) error {
	conn := b.pool.Get()
	defer conn.Close()

	vKey := fmt.Sprintf(keyVote, pollID)
	sKey := fmt.Sprintf(keyState, pollID)

	log.Debug("Redis: EVAL '%s' 2 %s %s %d %s", luaVoteScript, sKey, vKey, userID, object)
	result, err := redis.Int(conn.Do("EVAL", luaVoteScript, 2, sKey, vKey, userID, object))
	if err != nil {
		return fmt.Errorf("executing luaVoteScript: %w", err)
	}

	log.Debug("Redis: Returned %d", result)
	switch result {
	case 0:
		return nil
	case 1:
		return doesNotExistError{fmt.Errorf("poll is not started")}
	case 2:
		return stoppedError{fmt.Errorf("poll is stopped")}
	case 3:
		return doupleVoteError{fmt.Errorf("user has voted")}
	default:
		return fmt.Errorf("lua returned with %d", result)
	}
}

// Stop ends a poll.
//
// It returns all vote objects.
func (b *Backend) Stop(ctx context.Context, pollID int) ([][]byte, error) {
	conn := b.pool.Get()
	defer conn.Close()

	vKey := fmt.Sprintf(keyVote, pollID)
	sKey := fmt.Sprintf(keyState, pollID)

	log.Debug("SET %s 2 XX", sKey)
	_, err := redis.String(conn.Do("SET", sKey, "2", "XX"))
	if err != nil {
		if err == redis.ErrNil {
			return nil, doesNotExistError{fmt.Errorf("poll does not exist")}
		}
		return nil, fmt.Errorf("set key %s to 2: %w", sKey, err)
	}

	log.Debug("REDIS: HVALS %s", vKey)
	voteObjects, err := redis.ByteSlices(conn.Do("HVALS", vKey))
	if err != nil {
		return nil, fmt.Errorf("getting vote objects from %s: %w", vKey, err)
	}
	if log.IsDebug() {
		results := make([]string, len(voteObjects))
		for i := range voteObjects {
			results[i] = string(voteObjects[i])
		}
		log.Debug("Redis: Recieved %v", results)
	}
	return voteObjects, nil
}

// Clear delete all information from a poll.
func (b *Backend) Clear(ctx context.Context, pollID int) error {
	conn := b.pool.Get()
	defer conn.Close()

	vKey := fmt.Sprintf(keyVote, pollID)
	sKey := fmt.Sprintf(keyState, pollID)

	log.Debug("REDIS: DEL %s %s", vKey, sKey)
	if _, err := conn.Do("DEL", vKey, sKey); err != nil {
		return fmt.Errorf("removing keys: %w", err)
	}
	return nil
}

type doesNotExistError struct {
	error
}

func (doesNotExistError) DoesNotExist() {}

type doupleVoteError struct {
	error
}

func (doupleVoteError) DoupleVote() {}

type stoppedError struct {
	error
}

func (stoppedError) Stopped() {}
