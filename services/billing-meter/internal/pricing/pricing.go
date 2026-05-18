package pricing

import "math"

const (
	PlivoPerMinuteINR     = 0.80
	AIComputePerMinuteINR = 0.35
	RecordingPerMinuteINR = 0.05
	TTSPerCharacterINR    = 0.00008
	STTPerSecondINR       = 0.004
	LLMPerTokenINR        = 0.0006
)

var WhatsAppCategoryINR = map[string]float64{
	"marketing":      0.78,
	"utility":        0.35,
	"authentication": 0.28,
}

func RoundPlivoSeconds(seconds int64) int64 {
	if seconds <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(seconds)/60.0)) * 60
}

func CallCostINR(seconds int64) (billedSeconds int64, cost float64) {
	billedSeconds = RoundPlivoSeconds(seconds)
	minutes := float64(billedSeconds) / 60.0
	return billedSeconds, minutes * (PlivoPerMinuteINR + AIComputePerMinuteINR + RecordingPerMinuteINR)
}

func WhatsAppCostINR(category string) float64 {
	if value, ok := WhatsAppCategoryINR[category]; ok {
		return value
	}
	return WhatsAppCategoryINR["utility"]
}
