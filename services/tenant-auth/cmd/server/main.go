package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/redis/go-redis/v9"

	"github.com/lead/services/tenant-auth/internal/ratelimit"
	"github.com/lead/services/tenant-auth/internal/service"
	"github.com/lead/services/tenant-auth/internal/store"
	"github.com/lead/services/tenant-auth/internal/vault"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	st, err := newStore()
	if err != nil {
		log.Error("store init", "error", err)
		os.Exit(1)
	}
	if closer, ok := st.(interface{ Close() }); ok {
		defer closer.Close()
	}

	rl, err := newLimiter()
	if err != nil {
		log.Error("rate limiter init", "error", err)
		os.Exit(1)
	}

	keys := vault.NewStatic("")
	svc := service.New(st, keys, rl)

	mux := http.NewServeMux()
	svc.Mount(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	addr := fmt.Sprintf(":%s", port)
	log.Info("tenant-auth service starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

func newStore() (store.Store, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return store.NewPostgres(dsn)
	}
	return store.NewFake(), nil
}

func newLimiter() (ratelimit.Limiter, error) {
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		return &ratelimit.NoOp{}, nil
	}
	opt, err := redis.ParseURL(redisAddr)
	if err != nil {
		return nil, err
	}
	return ratelimit.NewRedis(redis.NewClient(opt)), nil
}
