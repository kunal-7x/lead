package model

import "time"

type ConsentBasis string

const (
	ConsentBasisExplicit   ConsentBasis = "explicit"
	ConsentBasisLegitimate ConsentBasis = "legitimate_interest"
	ConsentBasisContract   ConsentBasis = "contract"
	ConsentBasisLegal      ConsentBasis = "legal_obligation"
	ConsentBasisWithdrawal ConsentBasis = "withdrawal"
)

type Channel string

const (
	ChannelPhone    Channel = "phone"
	ChannelWhatsApp Channel = "whatsapp"
	ChannelEmail    Channel = "email"
	ChannelSMS      Channel = "sms"
)

type ConsentRecord struct {
	ID            string
	LeadID        string
	Basis         ConsentBasis
	Source        string
	NoticeVersion string
	EvidenceURL   string
	CreatedAt     time.Time
	PIIRedacted   bool
	RedactedAt    *time.Time
}

type Suppression struct {
	ID        string
	Phone     string
	Reason    string
	TicketID  string
	CreatedAt time.Time
}

type WhatsAppOptOut struct {
	ID        string
	LeadID    string
	Channel   Channel
	Reason    string
	CreatedAt time.Time
}

// KnowledgeBase represents an approved RERA project knowledge base for a campaign.
type KnowledgeBase struct {
	ID             string
	CampaignID     string
	Approved       bool
	RERANumber     string
	HasBrochure    bool
	HasPriceSheet  bool
	HasDisclaimers bool
	Version        string
}

type OutreachResult struct {
	Allowed bool
	Reason  string
}
