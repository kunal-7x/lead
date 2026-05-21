// freeswitch-bridge: ESL event listener + recording upload worker for FreeSWITCH.
//
// Phase C8: wires real DO Spaces (hot tier) + Backblaze B2 (WORM archive)
// recording uploaders. Recordings are pulled from disk or remote URL,
// uploaded SSE-encrypted to Spaces, then archived COMPLIANCE-locked to B2.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lead/libs/go/events"
	"github.com/lead/services/freeswitch-bridge/internal/audiostream"
	"github.com/lead/services/freeswitch-bridge/internal/esl"
	"github.com/lead/services/freeswitch-bridge/internal/recording"
	"github.com/lead/services/freeswitch-bridge/internal/store"
)

func main() {
	addr := envOr("FREESWITCH_BRIDGE_ADDR", ":8109")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	reportStartupReadiness()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok"}`)
	})
	audioProxy := audiostream.NewProxy(envOr("VOICE_AGENT_WS_URL", "ws://localhost:8200/ws/audio"))
	audioProxy.PlaybackFormat = envOr("FREESWITCH_PLAYBACK_FORMAT", "streamAudio")
	mux.Handle("/ws/audio", audioProxy)
	mux.Handle("/ws/audio/", audioProxy)

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
	var uploadSink esl.RecordingUploader = noopUploader{}

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
		uploadSink = worker
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

	if enabled := envOr("FREESWITCH_ESL_ENABLED", "true"); enabled != "false" && enabled != "0" {
		listener := esl.NewListener(pub, makeSessionStore(pgStore), uploadSink)
		cfg := esl.ClientConfig{
			Addr:              net.JoinHostPort(envOr("FREESWITCH_HOST", "127.0.0.1"), envOr("FREESWITCH_ESL_PORT", "8021")),
			Password:          envOr("ESL_PASSWORD", "ClueCon"),
			ReconnectInterval: durationEnv("FREESWITCH_ESL_RECONNECT_INTERVAL", 2*time.Second),
		}
		go func() {
			if err := esl.Run(ctx, cfg, listener.Handle); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "freeswitch-bridge esl: %v\n", err)
			}
		}()
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

func makeSessionStore(pg *store.PostgresStore) esl.SessionStore {
	if pg == nil {
		return noopSessionStore{}
	}
	return pg
}

type noopSessionStore struct{}

func (noopSessionStore) SetSessionStarted(context.Context, string) error { return nil }
func (noopSessionStore) SetSessionEnded(context.Context, string) error   { return nil }

type noopUploader struct{}

func (noopUploader) Enqueue(context.Context, string, string, string) error { return nil }

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s=%q: %v (using %s)\n", key, raw, err, fallback)
		return fallback
	}
	return d
}

func reportStartupReadiness() {
	if missing := missingEnv("NATS_URL", "ESL_PASSWORD", "VOICE_AGENT_WS_URL"); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge readiness: missing local env %v (service may run degraded)\n", missing)
	}
	if missing := missingEnv("JIO_GATEWAY_IP"); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge go-live: missing Jio SIP trunk env %v (live PSTN blocked)\n", missing)
	}
	if missing := missingEnv("DO_SPACES_KEY", "DO_SPACES_SECRET", "DO_SPACES_BUCKET"); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge go-live: missing recording hot-tier env %v (recording upload disabled)\n", missing)
	}
	if missing := missingEnv("B2_KEY", "B2_SECRET", "B2_BUCKET"); len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge go-live: missing recording archive env %v (WORM archive disabled)\n", missing)
	}
}

func missingEnv(keys ...string) []string {
	var missing []string
	for _, key := range keys {
		if isMissingConfigValue(os.Getenv(key)) {
			missing = append(missing, key)
		}
	}
	return missing
}

func isMissingConfigValue(value string) bool {
	v := strings.TrimSpace(value)
	return v == "" || v == "__placeholder__" || v == "placeholder" || v == "changeme"
}
