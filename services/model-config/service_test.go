package modelconfig_test

import (
	"context"
	"testing"

	"github.com/lead/services/model-config/internal/model"
	"github.com/lead/services/model-config/internal/registry"
	"github.com/lead/services/model-config/internal/service"
	"github.com/lead/services/model-config/internal/store"
)

func TestCurrentUsesDefaults(t *testing.T) {
	svc := service.New(store.NewFake())
	current, err := svc.Current(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.Global.LLM != registry.DefaultLLM || current.Global.STT != registry.DefaultSTT || current.Global.TTS != registry.DefaultTTS {
		t.Fatalf("unexpected defaults: %#v", current.Global)
	}
}

func TestUpdateGlobalWritesRedisKeys(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	err := svc.UpdateGlobal(ctx, model.GlobalSelection{LLM: "mistral_7b", STT: "groq_whisper", TTS: "kokoro"})
	if err != nil {
		t.Fatalf("update global: %v", err)
	}
	assertKey(t, st, "llm:global_model", "mistral_7b")
	assertKey(t, st, "stt:global_engine", "groq_whisper")
	assertKey(t, st, "tts:global_engine", "kokoro")
}

func TestTenantOverrideRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	svc := service.New(st)
	if _, err := svc.UpdateTenant(ctx, "tenant-a", model.GlobalSelection{LLM: "openrouter", TTS: "elevenlabs"}); err != nil {
		t.Fatalf("update tenant: %v", err)
	}
	current, err := svc.Current(ctx)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if len(current.Tenants) != 1 || current.Tenants[0].TenantID != "tenant-a" || current.Tenants[0].LLM != "openrouter" || current.Tenants[0].TTS != "elevenlabs" {
		t.Fatalf("unexpected tenant overrides: %#v", current.Tenants)
	}
	if err := svc.DeleteTenant(ctx, "tenant-a"); err != nil {
		t.Fatalf("delete tenant: %v", err)
	}
	current, _ = svc.Current(ctx)
	if len(current.Tenants) != 0 {
		t.Fatalf("expected override removal, got %#v", current.Tenants)
	}
}

func TestInvalidModelRejected(t *testing.T) {
	svc := service.New(store.NewFake())
	err := svc.UpdateGlobal(context.Background(), model.GlobalSelection{LLM: "missing", STT: "sarvam", TTS: "sarvam_bulbul"})
	if err == nil {
		t.Fatal("expected invalid model error")
	}
}

func TestHealthReportsGPUWarning(t *testing.T) {
	svc := service.New(store.NewFake())
	svc.SetEnv(func(key string) string {
		if key == "GROQ_API_KEY" {
			return "set"
		}
		return ""
	})
	health := svc.Health(context.Background())
	var foundGroq, foundGPUWarning bool
	for _, item := range health {
		if item.ID == "groq_llama" && item.Online {
			foundGroq = true
		}
		if item.ID == "qwen3_32b" && item.Warning != "" {
			foundGPUWarning = true
		}
	}
	if !foundGroq || !foundGPUWarning {
		t.Fatalf("unexpected health: %#v", health)
	}
}

func assertKey(t *testing.T, st *store.Fake, key, want string) {
	t.Helper()
	got, ok, err := st.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get key: %v", err)
	}
	if !ok || got != want {
		t.Fatalf("key %s = %q, %v; want %q, true", key, got, ok, want)
	}
}
