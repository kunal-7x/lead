package compliance

import (
	"context"
	"time"

	"github.com/lead/services/consent-compliance/internal/model"
	"github.com/lead/services/consent-compliance/internal/store"
)

// WindowConfig defines the permitted calling window.
type WindowConfig struct {
	StartHour int // inclusive, local time
	EndHour   int // exclusive, local time (21 means last call at 20:59)
	Location  *time.Location
}

// DefaultWindowConfig returns the default 09:00–21:00 IST window.
func DefaultWindowConfig() WindowConfig {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return WindowConfig{
		StartHour: 9,
		EndHour:   21,
		Location:  ist,
	}
}

// CheckCallingWindow returns whether outreach is permitted at time t.
func CheckCallingWindow(t time.Time, cfg WindowConfig) model.OutreachResult {
	local := t.In(cfg.Location)
	h := local.Hour()
	if h < cfg.StartHour || h >= cfg.EndHour {
		return model.OutreachResult{Allowed: false, Reason: "calling_window"}
	}
	return model.OutreachResult{Allowed: true}
}

// CheckHoliday returns a blocked result if t falls on any date in holidays.
func CheckHoliday(t time.Time, holidays []time.Time) model.OutreachResult {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	y, m, d := t.In(ist).Date()
	for _, h := range holidays {
		hy, hm, hd := h.In(ist).Date()
		if y == hy && m == hm && d == hd {
			return model.OutreachResult{Allowed: false, Reason: "holiday"}
		}
	}
	return model.OutreachResult{Allowed: true}
}

// CheckTRAI verifies TRAI 140-series CLI requirement for real-estate promo calls.
// Real-estate phone outreach must originate from a 140-series number.
func CheckTRAI(channel model.Channel, campaignType, callerID string) model.OutreachResult {
	if campaignType == "real_estate" && channel == model.ChannelPhone {
		if len(callerID) < 3 || callerID[:3] != "140" {
			return model.OutreachResult{Allowed: false, Reason: "trai_140_series"}
		}
	}
	return model.OutreachResult{Allowed: true}
}

// CheckRERAGate blocks outreach for real-estate campaigns without a fully-approved KB.
// A valid KB must have: Approved=true, RERANumber, brochure, price sheet, and disclaimers.
func CheckRERAGate(ctx context.Context, s store.Store, campaignID, campaignType string) model.OutreachResult {
	if campaignType != "real_estate" {
		return model.OutreachResult{Allowed: true}
	}
	kb, err := s.GetApprovedKB(ctx, campaignID)
	if err != nil || kb == nil {
		return model.OutreachResult{Allowed: false, Reason: "rera_gate"}
	}
	if !kb.Approved || kb.RERANumber == "" || !kb.HasBrochure || !kb.HasPriceSheet || !kb.HasDisclaimers {
		return model.OutreachResult{Allowed: false, Reason: "rera_gate"}
	}
	return model.OutreachResult{Allowed: true}
}

// OutreachRequest bundles all parameters for CheckOutreachAllowed.
type OutreachRequest struct {
	LeadID       string
	Phone        string
	Channel      model.Channel
	AtTime       time.Time
	CampaignID   string
	CampaignType string
	CallerID     string
	Holidays     []time.Time
	WindowCfg    WindowConfig
}

// CheckOutreachAllowed runs all compliance gates in priority order and returns
// the first blocking reason, or {Allowed: true} if all gates pass.
func CheckOutreachAllowed(ctx context.Context, s store.Store, req OutreachRequest) model.OutreachResult {
	// 1. Suppression list
	if req.Phone != "" {
		suppressed, err := s.IsSuppressed(ctx, req.Phone)
		if err == nil && suppressed {
			return model.OutreachResult{Allowed: false, Reason: "suppression"}
		}
	}

	// 2. Per-lead opt-out
	if req.LeadID != "" {
		opted, err := s.IsOptedOut(ctx, req.LeadID, req.Channel)
		if err == nil && opted {
			return model.OutreachResult{Allowed: false, Reason: "opt_out"}
		}
	}

	// 3. Calling window
	if !req.AtTime.IsZero() {
		if result := CheckCallingWindow(req.AtTime, req.WindowCfg); !result.Allowed {
			return result
		}
	}

	// 4. National/state holiday
	if !req.AtTime.IsZero() && len(req.Holidays) > 0 {
		if result := CheckHoliday(req.AtTime, req.Holidays); !result.Allowed {
			return result
		}
	}

	// 5. TRAI 140-series
	if result := CheckTRAI(req.Channel, req.CampaignType, req.CallerID); !result.Allowed {
		return result
	}

	// 6. RERA project gate
	if req.CampaignID != "" && req.CampaignType == "real_estate" {
		if result := CheckRERAGate(ctx, s, req.CampaignID, req.CampaignType); !result.Allowed {
			return result
		}
	}

	return model.OutreachResult{Allowed: true}
}
