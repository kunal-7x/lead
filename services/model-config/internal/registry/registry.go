package registry

import (
	"fmt"

	"github.com/lead/services/model-config/internal/model"
)

const (
	DefaultLLM = "groq_llama"
	DefaultSTT = "sarvam"
	DefaultTTS = "sarvam_bulbul"
)

var engines = []model.Engine{
	{ID: "groq_llama", Name: "Groq Llama 3.x", Kind: model.KindLLM, Type: model.EngineAPI, CostPerCallINR: 0.11, EndpointEnvVar: "GROQ_API_KEY"},
	{ID: "qwen3_32b", Name: "Qwen3 32B vLLM", Kind: model.KindLLM, Type: model.EngineSelfHosted, CostPerCallINR: 0.04, RequiresGPU: true, EndpointEnvVar: "QWEN3_32B_VLLM_URL"},
	{ID: "llama3_70b", Name: "Llama 3.3 70B Q4 vLLM", Kind: model.KindLLM, Type: model.EngineSelfHosted, CostPerCallINR: 0.08, RequiresGPU: true, EndpointEnvVar: "LLAMA3_70B_VLLM_URL"},
	{ID: "mistral_7b", Name: "Mistral 7B vLLM", Kind: model.KindLLM, Type: model.EngineSelfHosted, CostPerCallINR: 0.02, RequiresGPU: true, EndpointEnvVar: "MISTRAL_7B_VLLM_URL"},
	{ID: "sarvam_105b", Name: "Sarvam 105B", Kind: model.KindLLM, Type: model.EngineAPI, CostPerCallINR: 0.16, EndpointEnvVar: "SARVAM_API_KEY"},
	{ID: "openai_gpt4o", Name: "OpenAI GPT-4o", Kind: model.KindLLM, Type: model.EngineAPI, CostPerCallINR: 0.42, EndpointEnvVar: "OPENAI_API_KEY"},
	{ID: "anthropic_claude", Name: "Anthropic Claude Sonnet", Kind: model.KindLLM, Type: model.EngineAPI, CostPerCallINR: 0.48, EndpointEnvVar: "ANTHROPIC_API_KEY"},
	{ID: "google_gemini", Name: "Google Gemini Flash", Kind: model.KindLLM, Type: model.EngineAPI, CostPerCallINR: 0.19, EndpointEnvVar: "GOOGLE_GEMINI_API_KEY"},
	{ID: "openrouter", Name: "OpenRouter", Kind: model.KindLLM, Type: model.EngineAPI, CostPerCallINR: 0.22, EndpointEnvVar: "OPENROUTER_API_KEY"},

	{ID: "sarvam", Name: "Sarvam Saarika", Kind: model.KindSTT, Type: model.EngineAPI, CostPerCallINR: 0.05, EndpointEnvVar: "SARVAM_API_KEY"},
	{ID: "indicconformer", Name: "AI4Bharat IndicConformer", Kind: model.KindSTT, Type: model.EngineSelfHosted, CostPerCallINR: 0.01, EndpointEnvVar: "INDICCONFORMER_URL"},
	{ID: "faster_whisper", Name: "faster-whisper", Kind: model.KindSTT, Type: model.EngineSelfHosted, CostPerCallINR: 0.02, RequiresGPU: true, EndpointEnvVar: "FASTER_WHISPER_URL"},
	{ID: "groq_whisper", Name: "Groq Whisper", Kind: model.KindSTT, Type: model.EngineAPI, CostPerCallINR: 0.07, EndpointEnvVar: "GROQ_API_KEY"},

	{ID: "sarvam_bulbul", Name: "Sarvam Bulbul", Kind: model.KindTTS, Type: model.EngineAPI, CostPerCallINR: 0.05, EndpointEnvVar: "SARVAM_API_KEY"},
	{ID: "indic_parler", Name: "Indic Parler-TTS", Kind: model.KindTTS, Type: model.EngineSelfHosted, CostPerCallINR: 0.02, RequiresGPU: true, EndpointEnvVar: "INDIC_PARLER_URL"},
	{ID: "indicf5", Name: "IndicF5", Kind: model.KindTTS, Type: model.EngineSelfHosted, CostPerCallINR: 0.03, RequiresGPU: true, EndpointEnvVar: "INDICF5_URL"},
	{ID: "kokoro", Name: "Kokoro TTS", Kind: model.KindTTS, Type: model.EngineSelfHosted, CostPerCallINR: 0.01, EndpointEnvVar: "KOKORO_URL"},
	{ID: "elevenlabs", Name: "ElevenLabs", Kind: model.KindTTS, Type: model.EngineAPI, CostPerCallINR: 0.28, EndpointEnvVar: "ELEVENLABS_API_KEY"},
}

func All() []model.Engine {
	out := make([]model.Engine, len(engines))
	copy(out, engines)
	return out
}

func Available() model.AvailableModels {
	return model.AvailableModels{
		LLM: ByKind(model.KindLLM),
		STT: ByKind(model.KindSTT),
		TTS: ByKind(model.KindTTS),
	}
}

func ByKind(kind model.EngineKind) []model.Engine {
	var out []model.Engine
	for _, engine := range engines {
		if engine.Kind == kind {
			out = append(out, engine)
		}
	}
	return out
}

func Find(kind model.EngineKind, id string) (model.Engine, bool) {
	for _, engine := range engines {
		if engine.Kind == kind && engine.ID == id {
			return engine, true
		}
	}
	return model.Engine{}, false
}

func Validate(kind model.EngineKind, id string) error {
	if id == "" {
		return fmt.Errorf("%s model is required", kind)
	}
	if _, ok := Find(kind, id); !ok {
		return fmt.Errorf("unsupported %s model %q", kind, id)
	}
	return nil
}

func ValidateIfSet(kind model.EngineKind, id string) error {
	if id == "" {
		return nil
	}
	return Validate(kind, id)
}
