// Postgres-backed scanner for recording_upload_queue + retention cron.
package recording

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueScanner pulls pending rows from recording_upload_queue and feeds
// them into the Worker. Rows are claimed atomically so two replicas don't
// process the same job.
type QueueScanner struct {
	db     *pgxpool.Pool
	worker *Worker
	httpc  *http.Client
}

func NewQueueScanner(db *pgxpool.Pool, worker *Worker) *QueueScanner {
	return &QueueScanner{
		db:     db,
		worker: worker,
		httpc:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Run polls every `interval` until ctx cancels.
func (s *QueueScanner) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := s.tick(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "queue tick: %v\n", err)
			}
			if n > 0 {
				s.worker.DrainOnce(ctx)
			}
		}
	}
}

// tick claims up to 16 pending rows, downloads (if remote URL), enqueues.
func (s *QueueScanner) tick(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `
		UPDATE recording_upload_queue
		SET status = 'claimed', updated_at = now(), attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM recording_upload_queue
			WHERE status = 'pending' AND attempts < 5
			ORDER BY created_at
			LIMIT 16
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, tenant_id, session_id, local_path
	`)
	if err != nil {
		return 0, fmt.Errorf("claim rows: %w", err)
	}
	defer rows.Close()

	type job struct{ id, tenant, sess, path string }
	var claimed []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.tenant, &j.sess, &j.path); err != nil {
			return 0, fmt.Errorf("scan: %w", err)
		}
		claimed = append(claimed, j)
	}

	for _, j := range claimed {
		local := j.path
		if isRemoteURL(j.path) {
			tmp, err := s.downloadToTemp(ctx, j.path, j.sess)
			if err != nil {
				_ = s.markFailed(ctx, j.id, err.Error())
				continue
			}
			local = tmp
		}
		_ = s.worker.Enqueue(ctx, j.tenant, j.sess, local)
	}
	return len(claimed), nil
}

func (s *QueueScanner) downloadToTemp(ctx context.Context, srcURL, sessionID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("download %s: %d", srcURL, resp.StatusCode)
	}
	dir := os.TempDir()
	out, err := os.CreateTemp(dir, "rec-"+safeName(sessionID)+"-*.wav")
	if err != nil {
		return "", err
	}
	defer out.Close()
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return "", werr
			}
		}
		if rerr != nil {
			break
		}
	}
	return out.Name(), nil
}

func (s *QueueScanner) markFailed(ctx context.Context, id, reason string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE recording_upload_queue
		SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
		    last_error = $2, updated_at = now()
		WHERE id = $1
	`, id, reason)
	return err
}

// MarkUploaded is called by the Worker after a successful Spaces upload to
// move the row out of the pending state.
func (s *QueueScanner) MarkUploaded(ctx context.Context, id, spacesKey string, size int64) error {
	_, err := s.db.Exec(ctx, `
		UPDATE recording_upload_queue
		SET status = 'uploaded', spaces_key = $2, size_bytes = $3, updated_at = now()
		WHERE id = $1
	`, id, spacesKey, size)
	return err
}

func isRemoteURL(p string) bool {
	if p == "" {
		return false
	}
	u, err := url.Parse(p)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func safeName(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return r.Replace(s)
}

// ----- Retention cron -----

// RetentionCron deletes Spaces objects older than hotRetention. B2 objects
// are skipped — they are immutable under Object Lock until the bucket's
// retention period expires.
type RetentionCron struct {
	spaces       *spacesClient
	hotRetention time.Duration
	prefix       string
}

func NewRetentionCron(spaces *spacesClient, hotRetentionDays int, prefix string) *RetentionCron {
	if hotRetentionDays <= 0 {
		hotRetentionDays = 90
	}
	return &RetentionCron{
		spaces:       spaces,
		hotRetention: time.Duration(hotRetentionDays) * 24 * time.Hour,
		prefix:       prefix,
	}
}

// RunOnce performs one retention sweep. Designed to be invoked daily.
func (r *RetentionCron) RunOnce(ctx context.Context) (deleted int, err error) {
	cutoff := time.Now().UTC().Add(-r.hotRetention)
	keys, err := r.spaces.ListOlderThan(ctx, r.prefix, cutoff)
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		if err := r.spaces.Delete(ctx, k); err != nil {
			return deleted, fmt.Errorf("retention delete %s: %w", k, err)
		}
		deleted++
	}
	return deleted, nil
}

// RunDaily blocks and runs the cron once a day until ctx cancels.
func (r *RetentionCron) RunDaily(ctx context.Context) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	// Run once on startup.
	if _, err := r.RunOnce(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "retention startup: %v\n", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := r.RunOnce(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "retention tick: %v\n", err)
			}
		}
	}
}
