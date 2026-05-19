// freeswitch-bridge: ESL event listener + recording upload worker for FreeSWITCH.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/lead/services/freeswitch-bridge/internal/store"
)

func main() {
	addr := os.Getenv("FREESWITCH_BRIDGE_ADDR")
	if addr == "" {
		addr = ":8109"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	ctx := context.Background()
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pg, err := store.NewPostgres(dsn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "freeswitch-bridge postgres: %v\n", err)
			os.Exit(1)
		}
		defer pg.Close()
		if err := pg.SaveInstance(ctx, store.Instance{
			ID:        "local",
			Host:      envOr("FREESWITCH_HOST", "localhost"),
			SIPPort:   5060,
			ESLPort:   8021,
			Healthy:   true,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "freeswitch-bridge save instance: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("freeswitch-bridge listening on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
