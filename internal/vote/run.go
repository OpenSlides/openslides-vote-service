package vote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/OpenSlides/openslides-autoupdate-service/pkg/auth"
	"github.com/OpenSlides/openslides-autoupdate-service/pkg/datastore"
	messageBusRedis "github.com/OpenSlides/openslides-autoupdate-service/pkg/redis"
	"github.com/OpenSlides/openslides-vote-service/internal/backends/memory"
	"github.com/OpenSlides/openslides-vote-service/internal/backends/redis"
	"github.com/OpenSlides/openslides-vote-service/internal/log"
)

const authDebugKey = "auth-dev-key"

// Run starts the http server.
//
// The server is automaticly closed when ctx is done.
//
// The service is configured by the argument `environment`. It expect strings in
// the format `KEY=VALUE`, like the output from `os.Environmen()`.
//
// log messages are written with the function given in the argument log. It has
// the signature from log.Printf().
func Run(ctx context.Context, environment []string, secret func(name string) (string, error)) error {
	env := defaultEnv(environment)

	errHandler := buildErrHandler()

	messageBus, err := buildMessageBus(env)
	if err != nil {
		return fmt.Errorf("building message bus: %w", err)
	}

	ds, err := buildDatastore(env, messageBus, ctx.Done(), errHandler)
	if err != nil {
		return fmt.Errorf("building datastore: %w", err)
	}

	auth, err := buildAuth(
		env,
		secret,
		messageBus,
		ctx.Done(),
		errHandler,
	)
	if err != nil {
		return fmt.Errorf("building auth: %w", err)
	}

	fastBackend, err := buildBackend(ctx, env, env["VOTE_BACKEND_FAST"])
	if err != nil {
		return fmt.Errorf("building fast backend: %w", err)
	}

	longBackend, err := buildBackend(ctx, env, env["VOTE_BACKEND_LONG"])
	if err != nil {
		return fmt.Errorf("building long backend: %w", err)
	}

	service := New(fastBackend, longBackend, ds)

	mux := http.NewServeMux()
	handleCreate(mux, service)
	handleStop(mux, service)
	handleClear(mux, service)
	handleVote(mux, service, auth)
	handleHealth(mux)

	listenAddr := ":" + env["VOTE_PORT"]
	srv := &http.Server{Addr: listenAddr, Handler: mux}

	// Shutdown logic in separate goroutine.
	wait := make(chan error)
	go func() {
		// Wait for the context to be closed.
		<-ctx.Done()

		if err := srv.Shutdown(context.Background()); err != nil {
			wait <- fmt.Errorf("HTTP server shutdown: %w", err)
			return
		}
		wait <- nil
	}()

	log.Info("Listen on %s", listenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("HTTP Server failed: %v", err)
	}

	return <-wait
}

// defaultEnv parses the environment (output from os.Environ()) and sets specific
// defaut values.
func defaultEnv(environment []string) map[string]string {
	env := map[string]string{
		"VOTE_HOST":         "",
		"VOTE_PORT":         "9013",
		"VOTE_BACKEND_FAST": "redis",
		"VOTE_BACKEND_LONG": "redis", // TODO: postgres
		"VOTE_REDIS_HOST":   "localhost",
		"VOTE_REDIS_PORT":   "6379",

		"DATASTORE_READER_HOST":     "localhost",
		"DATASTORE_READER_PORT":     "9010",
		"DATASTORE_READER_PROTOCOL": "http",

		"AUTH":          "fake",
		"AUTH_PROTOCOL": "http",
		"AUTH_HOST":     "localhost",
		"AUTH_PORT":     "9004",

		"MESSAGING":        "fake",
		"MESSAGE_BUS_HOST": "localhost",
		"MESSAGE_BUS_PORT": "6379",
		"REDIS_TEST_CONN":  "true",

		"OPENSLIDES_DEVELOPMENT": "false",
	}

	for _, value := range environment {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			panic(fmt.Sprintf("Invalid value from environment(): %s", value))
		}

		env[parts[0]] = parts[1]
	}
	return env
}

func secret(name string, getSecret func(name string) (string, error), dev bool) (string, error) {
	defaultSecrets := map[string]string{
		"auth_token_key":  authDebugKey,
		"auth_cookie_key": authDebugKey,
	}

	d, ok := defaultSecrets[name]
	if !ok {
		return "", fmt.Errorf("unknown secret %s", name)
	}

	s, err := getSecret(name)
	if err != nil {
		if !dev {
			return "", fmt.Errorf("can not read secret %s: %w", s, err)
		}
		s = d
	}
	return s, nil
}

func buildErrHandler() func(err error) {
	return func(err error) {
		var closing interface {
			Closing()
		}
		if !errors.As(err, &closing) {
			log.Info("Error: %v", err)
		}
	}
}

func buildDatastore(env map[string]string, receiver datastore.Updater, closed <-chan struct{}, errHandler func(error)) (*datastore.Datastore, error) {
	protocol := env["DATASTORE_READER_PROTOCOL"]
	host := env["DATASTORE_READER_HOST"]
	port := env["DATASTORE_READER_PORT"]
	url := protocol + "://" + host + ":" + port
	return datastore.New(url, closed, errHandler, receiver), nil
}

// buildAuth returns the auth service needed by the http server.
func buildAuth(
	env map[string]string,
	getSecret func(name string) (string, error),
	receiver auth.LogoutEventer,
	closed <-chan struct{},
	errHandler func(error),
) (authenticater, error) {
	method := env["AUTH"]
	switch method {
	case "ticket":
		log.Info("Auth Method: ticket")
		tokenKey, err := secret("auth_token_key", getSecret, env["OPENSLIDES_DEVELOPMENT"] != "false")
		if err != nil {
			return nil, fmt.Errorf("getting token secret: %w", err)
		}

		cookieKey, err := secret("auth_cookie_key", getSecret, env["OPENSLIDES_DEVELOPMENT"] != "false")
		if err != nil {
			return nil, fmt.Errorf("getting cookie secret: %w", err)
		}

		if tokenKey == authDebugKey || cookieKey == authDebugKey {
			log.Info("Auth with debug key")
		}

		protocol := env["AUTH_PROTOCOL"]
		host := env["AUTH_HOST"]
		port := env["AUTH_PORT"]
		url := protocol + "://" + host + ":" + port

		log.Info("Auth Service: %s", url)

		return auth.New(url, receiver, closed, errHandler, []byte(tokenKey), []byte(cookieKey))

	case "fake":
		log.Info("Auth Method: FakeAuth (User ID 1 for all requests)")
		return authStub(1), nil

	default:
		return nil, fmt.Errorf("unknown auth method %s", method)
	}
}

// authStub implements the authenticater interface. It allways returs the given
// user id.
type authStub int

// Authenticate does nothing.
func (a authStub) Authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	return r.Context(), nil
}

// FromContext returns the uid the object was initialiced with.
func (a authStub) FromContext(ctx context.Context) int {
	return int(a)
}

type messageBus interface {
	datastore.Updater
	auth.LogoutEventer
}

func buildMessageBus(env map[string]string) (messageBus, error) {
	serviceName := env["MESSAGING"]
	log.Info("Messaging Service: %s", serviceName)

	var conn messageBusRedis.Connection
	switch serviceName {
	case "redis":
		redisAddress := env["MESSAGE_BUS_HOST"] + ":" + env["MESSAGE_BUS_PORT"]
		c := messageBusRedis.NewConnection(redisAddress)
		if env["REDIS_TEST_CONN"] == "true" {
			if err := c.TestConn(); err != nil {
				return nil, fmt.Errorf("connect to redis: %w", err)
			}
		}

		conn = c

	case "fake":
		conn = messageBusRedis.BlockingConn{}
	default:
		return nil, fmt.Errorf("unknown messagin service %s", serviceName)
	}

	return &messageBusRedis.Redis{Conn: conn}, nil
}

func buildBackend(ctx context.Context, env map[string]string, name string) (Backend, error) {
	switch name {
	case "memory":
		return memory.New(), nil
	case "redis":
		addr := env["VOTE_REDIS_HOST"] + ":" + env["VOTE_REDIS_PORT"]
		b := redis.New(addr)
		b.Wait(ctx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return b, nil

	case "long":
		return nil, errors.New("TODO")

	default:
		return nil, fmt.Errorf("unknown backend %s", name)
	}
}
