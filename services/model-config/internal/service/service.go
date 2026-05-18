package service

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lead/services/model-config/internal/model"
	"github.com/lead/services/model-config/internal/registry"
	"github.com/lead/services/model-config/internal/store"
)

const (
	keyLLMGlobal = "llm:global_model"
	keySTTGlobal = "stt:global_engine"
	keyTTSGlobal = "tts:global_engine"
)

type Service struct {
	store store.Store
	env   func(string) string
	now   func() time.Time
}

func New(st store.Store) *Service {
	return &Service{
		store: st,
		env:   os.Getenv,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetEnv(env func(string) string) {
	if env != nil {
		s.env = env
	}
}

func (s *Service) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) Current(ctx context.Context) (model.CurrentConfig, error) {
	global, err := s.global(ctx)
	if err != nil {
		return model.CurrentConfig{}, err
	}
	overrides := map[string]*model.TenantOverride{}
	if err := s.collectTenantKeys(ctx, overrides, "llm:tenant:", ":model", func(override *model.TenantOverride, value string) {
		override.LLM = value
	}); err != nil {
		return model.CurrentConfig{}, err
	}
	if err := s.collectTenantKeys(ctx, overrides, "stt:tenant:", ":engine", func(override *model.TenantOverride, value string) {
		override.STT = value
	}); err != nil {
		return model.CurrentConfig{}, err
	}
	if err := s.collectTenantKeys(ctx, overrides, "tts:tenant:", ":engine", func(override *model.TenantOverride, value string) {
		override.TTS = value
	}); err != nil {
		return model.CurrentConfig{}, err
	}
	tenants := make([]model.TenantOverride, 0, len(overrides))
	for _, override := range overrides {
		tenants = append(tenants, *override)
	}
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].TenantID < tenants[j].TenantID })
	return model.CurrentConfig{Global: global, Tenants: tenants}, nil
}

func (s *Service) UpdateGlobal(ctx context.Context, selection model.GlobalSelection) error {
	if err := validateSelection(selection); err != nil {
		return err
	}
	if err := s.store.Set(ctx, keyLLMGlobal, selection.LLM); err != nil {
		return err
	}
	if err := s.store.Set(ctx, keySTTGlobal, selection.STT); err != nil {
		return err
	}
	return s.store.Set(ctx, keyTTSGlobal, selection.TTS)
}

func (s *Service) UpdateTenant(ctx context.Context, tenantID string, selection model.GlobalSelection) (model.TenantOverride, error) {
	if strings.TrimSpace(tenantID) == "" {
		return model.TenantOverride{}, errors.New("tenant id is required")
	}
	if selection.LLM == "" && selection.STT == "" && selection.TTS == "" {
		return model.TenantOverride{}, errors.New("at least one override is required")
	}
	if err := registry.ValidateIfSet(model.KindLLM, selection.LLM); err != nil {
		return model.TenantOverride{}, err
	}
	if err := registry.ValidateIfSet(model.KindSTT, selection.STT); err != nil {
		return model.TenantOverride{}, err
	}
	if err := registry.ValidateIfSet(model.KindTTS, selection.TTS); err != nil {
		return model.TenantOverride{}, err
	}
	if err := s.writeOptional(ctx, "llm:tenant:"+tenantID+":model", selection.LLM); err != nil {
		return model.TenantOverride{}, err
	}
	if err := s.writeOptional(ctx, "stt:tenant:"+tenantID+":engine", selection.STT); err != nil {
		return model.TenantOverride{}, err
	}
	if err := s.writeOptional(ctx, "tts:tenant:"+tenantID+":engine", selection.TTS); err != nil {
		return model.TenantOverride{}, err
	}
	return model.TenantOverride{
		TenantID:  tenantID,
		LLM:       selection.LLM,
		STT:       selection.STT,
		TTS:       selection.TTS,
		UpdatedAt: s.now(),
	}, nil
}

func (s *Service) DeleteTenant(ctx context.Context, tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("tenant id is required")
	}
	if err := s.store.Delete(ctx, "llm:tenant:"+tenantID+":model"); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, "stt:tenant:"+tenantID+":engine"); err != nil {
		return err
	}
	return s.store.Delete(ctx, "tts:tenant:"+tenantID+":engine")
}

func (s *Service) Available() model.AvailableModels {
	return registry.Available()
}

func (s *Service) Health(_ context.Context) []model.EngineHealth {
	engines := registry.All()
	out := make([]model.EngineHealth, 0, len(engines))
	for idx, engine := range engines {
		online := s.env(engine.EndpointEnvVar) != ""
		status := "offline"
		if online {
			status = "online"
		}
		latency := 0
		if online {
			latency = 70 + (idx % 7 * 18)
			if engine.Type == model.EngineSelfHosted {
				latency += 85
			}
		}
		health := model.EngineHealth{
			ID:             engine.ID,
			Name:           engine.Name,
			Kind:           engine.Kind,
			Online:         online,
			Status:         status,
			LatencyMS:      latency,
			P95LatencyMS:   latency + 35,
			RequiresGPU:    engine.RequiresGPU,
			EndpointEnvVar: engine.EndpointEnvVar,
		}
		if engine.RequiresGPU && !online {
			health.Warning = "GPU model endpoint is not reachable"
		}
		out = append(out, health)
	}
	return out
}

func (s *Service) global(ctx context.Context) (model.GlobalSelection, error) {
	llm, err := s.valueOrDefault(ctx, keyLLMGlobal, registry.DefaultLLM)
	if err != nil {
		return model.GlobalSelection{}, err
	}
	stt, err := s.valueOrDefault(ctx, keySTTGlobal, registry.DefaultSTT)
	if err != nil {
		return model.GlobalSelection{}, err
	}
	tts, err := s.valueOrDefault(ctx, keyTTSGlobal, registry.DefaultTTS)
	if err != nil {
		return model.GlobalSelection{}, err
	}
	return model.GlobalSelection{LLM: llm, STT: stt, TTS: tts}, nil
}

func (s *Service) valueOrDefault(ctx context.Context, key string, fallback string) (string, error) {
	value, ok, err := s.store.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if !ok || value == "" {
		return fallback, nil
	}
	return value, nil
}

func (s *Service) collectTenantKeys(ctx context.Context, overrides map[string]*model.TenantOverride, prefix string, suffix string, assign func(*model.TenantOverride, string)) error {
	keys, err := s.store.Keys(ctx, prefix)
	if err != nil {
		return err
	}
	for key, value := range keys {
		if !strings.HasSuffix(key, suffix) {
			continue
		}
		tenantID := strings.TrimSuffix(strings.TrimPrefix(key, prefix), suffix)
		if tenantID == "" {
			continue
		}
		override := overrides[tenantID]
		if override == nil {
			override = &model.TenantOverride{TenantID: tenantID, UpdatedAt: s.now()}
			overrides[tenantID] = override
		}
		assign(override, value)
	}
	return nil
}

func (s *Service) writeOptional(ctx context.Context, key string, value string) error {
	if value == "" {
		return s.store.Delete(ctx, key)
	}
	return s.store.Set(ctx, key, value)
}

func validateSelection(selection model.GlobalSelection) error {
	if err := registry.Validate(model.KindLLM, selection.LLM); err != nil {
		return err
	}
	if err := registry.Validate(model.KindSTT, selection.STT); err != nil {
		return err
	}
	return registry.Validate(model.KindTTS, selection.TTS)
}
