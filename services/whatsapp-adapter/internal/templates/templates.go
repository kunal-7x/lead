package templates

import (
	"fmt"

	"github.com/lead/services/whatsapp-adapter/internal/model"
)

func ValidateCategory(category model.TemplateCategory) error {
	switch category {
	case model.TemplateCategoryMarketing, model.TemplateCategoryUtility, model.TemplateCategoryAuthentication:
		return nil
	default:
		return fmt.Errorf("template category %q is not allowed", category)
	}
}

func Prebuilt(tenantID string) []model.Template {
	return []model.Template{
		{
			TenantID:  tenantID,
			Name:      "lead-warm-up",
			Language:  "en",
			Category:  model.TemplateCategoryMarketing,
			Body:      "Hi {{lead_name}}, thanks for your interest. We can help with {{project_name}}.",
			Status:    "draft",
			Variables: []string{"lead_name", "project_name"},
		},
		{
			TenantID:  tenantID,
			Name:      "site-visit-reminder",
			Language:  "en",
			Category:  model.TemplateCategoryUtility,
			Body:      "Reminder: your site visit for {{project_name}} is scheduled at {{visit_time}}.",
			Status:    "draft",
			Variables: []string{"project_name", "visit_time"},
		},
		{
			TenantID:  tenantID,
			Name:      "post-call-summary",
			Language:  "en",
			Category:  model.TemplateCategoryUtility,
			Body:      "Summary of our call: {{summary}}. Reply here for help.",
			Status:    "draft",
			Variables: []string{"summary"},
		},
		{
			TenantID:  tenantID,
			Name:      "handover-intro",
			Language:  "en",
			Category:  model.TemplateCategoryUtility,
			Body:      "Introducing {{owner_name}} from our team for your next steps.",
			Status:    "draft",
			Variables: []string{"owner_name"},
		},
	}
}
