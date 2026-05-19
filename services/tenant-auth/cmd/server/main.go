package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/redis/go-redis/v9"

	libsvault "github.com/lead/libs/go/vault"
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

	keys, vc := newKeyProvider(log)
	svc := service.New(st, keys, rl)
	if vc != nil {
		svc.SetRotator(vc)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	svc.Mount(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8101"
	}
	addr := fmt.Sprintf(":%s", port)
	log.Info("tenant-auth service starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}

// newKeyProvider returns a KeyProvider. When VAULT_ADDR is set it uses a
// Vault-backed provider with 5-second TTL cache (enables JWT rotation).
// Falls back to static env var so dev without Vault keeps working.
func newKeyProvider(log *slog.Logger) (vault.KeyProvider, *libsvault.Client) {
	addr := os.Getenv("VAULT_ADDR")
	token := os.Getenv("VAULT_TOKEN")
	if addr == "" || token == "" {
		return vault.NewStatic(""), nil
	}
	vc, err := libsvault.New(addr, token)
	if err != nil {
		log.Warn("vault client init failed — falling back to static key", "error", err)
		return vault.NewStatic(""), nil
	}
	log.Info("loaded secret from vault", "key", "JWT_SECRET", "path", "capsy/jwt")
	return vault.NewVaultProvider(vc, os.Getenv("JWT_SECRET")), vc
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
