package postgres

import (
	"bytes"
	"context"
	"database/sql/driver"
	_ "embed" // Needed for file embedding
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

//go:embed schema.sql
var schema string

// Backend holds the state of the backend.
//
// Has to be initializes with New().
type Backend struct {
	pool *pgxpool.Pool
}

// New creates a new connection pool.
func New(ctx context.Context, url string) (*Backend, error) {
	conf, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid connection url: %w", err)
	}
	conf.LazyConnect = true
	pool, err := pgxpool.ConnectConfig(ctx, conf)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	b := Backend{
		pool: pool,
	}

	return &b, nil
}

// Wait blocks until a connection to postgres can be established.
func (b *Backend) Wait(ctx context.Context, log func(format string, a ...interface{})) {
	for ctx.Err() == nil {
		err := b.pool.Ping(ctx)
		if err == nil {
			return
		}
		if log != nil {
			log("Waiting for postgres: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Migrate creates the database schema.
func (b *Backend) Migrate(ctx context.Context) error {
	if _, err := b.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}
	return nil
}

// Close closes all connections. It blocks, until all connection are closed.
func (b *Backend) Close() {
	b.pool.Close()
}

// Start starts a poll.
func (b *Backend) Start(ctx context.Context, pollID int) error {
	sql := `
	INSERT INTO poll (poll_id, stopped) VALUES ($1, false) ON CONFLICT DO NOTHING;
	`
	if _, err := b.pool.Exec(ctx, sql, pollID); err != nil {
		return fmt.Errorf("insert poll: %w", err)
	}
	return nil
}

// Vote adds a vote to a poll.
func (b *Backend) Vote(ctx context.Context, pollID int, userID int, object []byte) error {
	err := b.pool.BeginTxFunc(
		ctx,
		pgx.TxOptions{
			IsoLevel: "REPEATABLE READ",
		},
		func(tx pgx.Tx) error {
			sql := `
			SELECT id, stopped, user_ids 
			FROM poll
			WHERE poll_id = $1;
			`
			var dbPollID int
			var stopped bool
			var uIDs userIDs
			if err := tx.QueryRow(ctx, sql, pollID).Scan(&dbPollID, &stopped, &uIDs); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return doesNotExistError{fmt.Errorf("unknown poll")}
				}
				return fmt.Errorf("fetching poll data: %w", err)
			}

			if stopped {
				return stoppedError{fmt.Errorf("poll is stopped")}
			}

			if err := uIDs.add(int32(userID)); err != nil {
				return fmt.Errorf("adding userID to voted users: %w", err)
			}

			sql = "UPDATE poll SET user_ids = $1 WHERE poll_id = $2;"
			if _, err := tx.Exec(ctx, sql, uIDs, pollID); err != nil {
				return fmt.Errorf("writing user ids: %w", err)
			}

			sql = "INSERT INTO objects (poll_id, vote) VALUES ($1, $2);"
			if _, err := tx.Exec(ctx, sql, dbPollID, object); err != nil {
				return fmt.Errorf("writing vote: %w", err)
			}

			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("running transaction: %w", err)
	}
	return nil
}

// Stop ends a poll and returns all vote objects.
func (b *Backend) Stop(ctx context.Context, pollID int) ([][]byte, error) {
	var objects [][]byte
	err := b.pool.BeginTxFunc(
		ctx,
		pgx.TxOptions{
			IsoLevel: "REPEATABLE READ",
		},
		func(tx pgx.Tx) error {
			sql := "SELECT EXISTS(SELECT 1 FROM poll WHERE poll_id = $1);"

			var exists bool
			if err := tx.QueryRow(ctx, sql, pollID).Scan(&exists); err != nil {
				return fmt.Errorf("fetching poll exists: %w", err)
			}

			if !exists {
				return doesNotExistError{fmt.Errorf("Poll does not exist")}
			}

			sql = "UPDATE poll SET stopped = true WHERE poll_id = $1;"
			if _, err := tx.Exec(ctx, sql, pollID); err != nil {
				return fmt.Errorf("setting poll %d to stopped: %v", pollID, err)
			}

			sql = `
			SELECT Obj.vote 
			FROM poll P
			LEFT JOIN objects Obj ON Obj.poll_id = P.id
			WHERE P.poll_id= $1;
			`
			rows, err := tx.Query(ctx, sql, pollID)
			if err != nil {
				return fmt.Errorf("fetching vote objects: %w", err)
			}

			for rows.Next() {
				var bs []byte
				err = rows.Scan(&bs)
				if err != nil {
					return fmt.Errorf("parsind row: %w", err)
				}
				objects = append(objects, bs)
			}

			if err := rows.Err(); err != nil {
				return fmt.Errorf("parsing query rows: %w", err)
			}

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("running transaction: %w", err)
	}
	return objects, nil
}

// Clear removes all data about a poll from the database.
func (b *Backend) Clear(ctx context.Context, pollID int) error {
	sql := "DELETE FROM poll WHERE poll_id = $1"
	if _, err := b.pool.Exec(ctx, sql, pollID); err != nil {
		return fmt.Errorf("setting poll %d to stopped: %v", pollID, err)
	}
	return nil
}

type userIDs []int32

func (u *userIDs) Scan(src interface{}) error {
	if src == nil {
		*u = []int32{}
		return nil
	}

	bs, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("can not assign %v (%T) to userIDs", src, src)
	}

	// TODO: Add more test that this is working.
	ints := make([]int32, len(bs)/4)
	if err := binary.Read(bytes.NewReader(bs), binary.LittleEndian, &ints); err != nil {
		return fmt.Errorf("decoding user ids: %w", err)
	}
	*u = ints
	return nil

}

func (u userIDs) Value() (driver.Value, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, u); err != nil {
		return nil, fmt.Errorf("encoding user id %v: %w", u, err)
	}

	return buf.Bytes(), nil
}

// add adds the userID to the userIDs
func (u *userIDs) add(userID int32) error {
	// idx is either the index of userID or the place where it should be
	// inserted.
	ints := []int32(*u)
	idx := sort.Search(len(ints), func(i int) bool { return ints[i] >= userID })
	if idx < len(ints) && ints[idx] == userID {
		return doupleVoteError{fmt.Errorf("User has already voted")}
	}

	// Insert the index at the correct order.
	ints = append(ints[:idx], append([]int32{userID}, ints[idx:]...)...)
	*u = ints
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
