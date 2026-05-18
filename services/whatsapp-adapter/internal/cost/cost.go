package cost

import (
	"fmt"

	"github.com/lead/services/whatsapp-adapter/internal/model"
)

var TemplateCategoryCostINR = map[model.TemplateCategory]float64{
	model.TemplateCategoryMarketing:      0.78,
	model.TemplateCategoryUtility:        0.35,
	model.TemplateCategoryAuthentication: 0.28,
}

func TemplateCostINR(category model.TemplateCategory) (float64, error) {
	value, ok := TemplateCategoryCostINR[category]
	if !ok {
		return 0, fmt.Errorf("unsupported template category %q", category)
	}
	return value, nil
}
