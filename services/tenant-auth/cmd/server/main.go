package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/lead/services/tenant-auth/internal/ratelimit"
	"github.com/lead/services/tenant-auth/internal/service"
	"github.com/lead/services/tenant-auth/internal/store"
	"github.com/lead/services/tenant-auth/internal/vault"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Error("DATABASE_URL not set")
		os.Exit(1)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Error("connect db", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "redis://localhost:6379"
	}
	opt, err := redis.ParseURL(redisAddr)
	if err != nil {
		log.Error("parse redis url", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(opt)

	pg := store.NewPG(pool)
	keys := vault.NewStatic("")
	rl := ratelimit.NewRedis(rdb)
	svc := service.New(pg, keys, rl)

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
