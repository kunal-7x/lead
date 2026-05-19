package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lead/libs/go/pgkv"
	"github.com/lead/services/scheduler/internal/model"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_scheduler")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) AddLeads(ctx context.Context, leads []*model.Lead) error {
	for _, lead := range leads {
		cp := *lead
		if cp.Status == "" {
			cp.Status = model.LeadStatusPending
		}
		if err := p.kv.Put(ctx, "leads", cp.ID, cp); err != nil {
			return err
		}
	}
	return nil
}

func (p *PostgresStore) PickAndClaim(ctx context.Context, campaignID, workerID string, batchSize int, now time.Time, leaseTTL time.Duration) ([]*model.Lead, error) {
	leads, err := pgkv.List[model.Lead](ctx, p.kv, "leads")
	if err != nil {
		return nil, err
	}
	var eligible []model.Lead
	for _, lead := range leads {
		if lead.CampaignID != campaignID || lead.Status != model.LeadStatusPending {
			continue
		}
		if !lead.ProcessingUntil.IsZero() && lead.ProcessingUntil.After(now) {
			continue
		}
		eligible = append(eligible, lead)
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].PriorityScore != eligible[j].PriorityScore {
			return eligible[i].PriorityScore > eligible[j].PriorityScore
		}
		if eligible[i].LastAttemptAt.Equal(eligible[j].LastAttemptAt) {
			return eligible[i].ID < eligible[j].ID
		}
		return eligible[i].LastAttemptAt.Before(eligible[j].LastAttemptAt)
	})
	if batchSize > len(eligible) {
		batchSize = len(eligible)
	}
	if batchSize < 0 {
		batchSize = 0
	}
	until := now.Add(leaseTTL)
	out := make([]*model.Lead, 0, batchSize)
	for _, lead := range eligible[:batchSize] {
		lead.ProcessingUntil = until
		lead.WorkerID = workerID
		lead.Status = model.LeadStatusProcessing
		if err := p.kv.Put(ctx, "leads", lead.ID, lead); err != nil {
			return nil, err
		}
		cp := lead
		out = append(out, &cp)
	}
	return out, nil
}

func (p *PostgresStore) MarkAttempt(ctx context.Context, callSessionID, outcome string) error {
	session, ok, err := pgkv.Get[map[string]string](ctx, p.kv, "call_sessions", callSessionID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("call session %q not found", callSessionID)
	}
	leadID := session["lead_id"]
	lead, ok, err := pgkv.Get[model.Lead](ctx, p.kv, "leads", leadID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("lead %q not found", leadID)
	}
	lead.LastAttemptAt = time.Now().UTC()
	lead.RetryCount++
	lead.ProcessingUntil = time.Time{}
	lead.WorkerID = ""
	switch outcome {
	case model.OutcomeConverted, model.OutcomeSuppressed, model.OutcomeMaxRetries:
		lead.Status = model.LeadStatusDone
	case model.OutcomeFailed:
		lead.Status = model.LeadStatusFailed
	default:
		lead.Status = model.LeadStatusPending
	}
	return p.kv.Put(ctx, "leads", lead.ID, lead)
}

func (p *PostgresStore) GetQueueDepth(ctx context.Context, tenantID string) (int, error) {
	leads, err := pgkv.List[model.Lead](ctx, p.kv, "leads")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, lead := range leads {
		if lead.TenantID == tenantID && lead.Status == model.LeadStatusPending {
			count++
		}
	}
	return count, nil
}

func (p *PostgresStore) SetCallSession(ctx context.Context, callSessionID, leadID string) error {
	return p.kv.Put(ctx, "call_sessions", callSessionID, map[string]string{"call_session_id": callSessionID, "lead_id": leadID})
}
