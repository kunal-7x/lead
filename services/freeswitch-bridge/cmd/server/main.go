// freeswitch-bridge: ESL event listener + recording upload worker for FreeSWITCH.
//
// Phase C8: wires real DO Spaces (hot tier) + Backblaze B2 (WORM archive)
// recording uploaders. Recordings are pulled from disk or remote URL,
// uploaded SSE-encrypted to Spaces, then archived COMPLIANCE-locked to B2.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/freeswitch-bridge/internal/recording"
	"github.com/lead/services/freeswitch-bridge/internal/store"
)

func main() {
	addr := envOr("FREESWITCH_BRIDGE_ADDR", ":8109")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	var pgStore *store.PostgresStore
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pg, err := store.NewPostgres(dsn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "freeswitch-bridge postgres: %v\n", err)
			os.Exit(1)
		}
		defer pg.Close()
		pgStore = pg
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

	// --- Recording pipeline (Spaces + B2 WORM) ---
	spaces, err := recording.NewSpaces(
		os.Getenv("DO_SPACES_KEY"),
		os.Getenv("DO_SPACES_SECRET"),
		envOr("DO_SPACES_REGION", "sgp1"),
		os.Getenv("DO_SPACES_BUCKET"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge spaces: %v (recording uploads disabled)\n", err)
	}

	retentionDays, _ := strconv.Atoi(os.Getenv("RECORDING_WORM_RETENTION_DAYS"))
	b2, err := recording.NewB2(
		os.Getenv("B2_KEY"),
		os.Getenv("B2_SECRET"),
		os.Getenv("B2_BUCKET"),
		retentionDays,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge b2: %v (WORM archive disabled)\n", err)
	}

	pub := makePublisher()

	if spaces != nil {
		opts := []recording.WorkerOption{}
		if b2 != nil {
			opts = append(opts, recording.WithArchiver(b2))
		}
		recStore := makeRecordingStore(pgStore)
		if as, ok := recStore.(recording.ArchiveStore); ok {
			opts = append(opts, recording.WithArchiveStore(as))
		}
		worker := recording.NewWorker(spaces, pub, recStore, opts...)
		// Drain queue every 5s.
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if errs := worker.DrainOnce(ctx); len(errs) > 0 {
						for _, e := range errs {
							fmt.Fprintf(os.Stderr, "recording drain: %v\n", e)
						}
					}
				}
			}
		}()

		// Daily retention sweep.
		hotDays, _ := strconv.Atoi(os.Getenv("RECORDING_HOT_RETENTION_DAYS"))
		cron := recording.NewRetentionCron(spaces, hotDays, "")
		go cron.RunDaily(ctx)
	}

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		fmt.Printf("freeswitch-bridge listening on %s\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "freeswitch-bridge: %v\n", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_ = srv.Shutdown(shutdownCtx)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// natsPublisher adapts events.Publisher to recording.Publisher (3-arg signature).
type natsPublisher struct{ p events.Publisher }

func (n *natsPublisher) Publish(ctx context.Context, subject string, payload []byte) error {
	return n.p.Publish(ctx, subject, payload)
}

func makePublisher() recording.Publisher {
	url := os.Getenv("NATS_URL")
	if url == "" {
		return recording.NewFakePublisher()
	}
	js, err := events.New(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge nats: %v (using in-memory publisher)\n", err)
		return recording.NewFakePublisher()
	}
	return &natsPublisher{p: js}
}

func makeRecordingStore(pg *store.PostgresStore) recording.RecordingStore {
	if pg == nil {
		return recording.NewFakeRecordingStore()
	}
	return pg
}
