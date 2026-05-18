package model

import "time"

type EngineKind string

const (
	KindLLM EngineKind = "llm"
	KindSTT EngineKind = "stt"
	KindTTS EngineKind = "tts"
)

type EngineType string

const (
	EngineAPI        EngineType = "api"
	EngineSelfHosted EngineType = "self_hosted"
)

type Engine struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Kind           EngineKind `json:"kind"`
	Type           EngineType `json:"type"`
	CostPerCallINR float64    `json:"cost_per_call"`
	RequiresGPU    bool       `json:"requires_gpu"`
	EndpointEnvVar string     `json:"endpoint_env_var"`
}

type GlobalSelection struct {
	LLM string `json:"llm"`
	STT string `json:"stt"`
	TTS string `json:"tts"`
}

type TenantOverride struct {
	TenantID  string    `json:"tenant_id"`
	LLM       string    `json:"llm,omitempty"`
	STT       string    `json:"stt,omitempty"`
	TTS       string    `json:"tts,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CurrentConfig struct {
	Global  GlobalSelection  `json:"global"`
	Tenants []TenantOverride `json:"tenants"`
}

type AvailableModels struct {
	LLM []Engine `json:"llm"`
	STT []Engine `json:"stt"`
	TTS []Engine `json:"tts"`
}

type EngineHealth struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Kind           EngineKind `json:"kind"`
	Online         bool       `json:"online"`
	Status         string     `json:"status"`
	LatencyMS      int        `json:"latency_ms"`
	P95LatencyMS   int        `json:"p95_latency_ms"`
	RequiresGPU    bool       `json:"requires_gpu"`
	EndpointEnvVar string     `json:"endpoint_env_var"`
	Warning        string     `json:"warning,omitempty"`
}
