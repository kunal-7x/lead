package events

// Stream and subject definitions for Capsy event flows.
//
// Streams are created on-demand when a Publisher is constructed. Each stream
// owns a wildcard subject pattern; concrete subjects are documented in the
// constants below.
//
// Wildcard layout (all subjects use "." as separator):
//
//	call.*            — call lifecycle (initiated, ringing, answered, completed)
//	lead.*            — lead lifecycle (created, scored, hot.detected)
//	handoff.*         — handoff requested/completed
//	wa.*              — WhatsApp template sent/delivered/failed
//	billing.*         — usage events from billing-meter
//	campaign.*        — campaign lifecycle (launched, paused, completed)
//	site.*            — site visits scheduled/completed
//	provider.*        — provider webhook normalization
type Subject = string

const (
	// Call subjects.
	SubjectCallInitiated   Subject = "call.initiated"
	SubjectCallAnswered    Subject = "call.answered"
	SubjectCallCompleted   Subject = "call.completed"
	SubjectCallRecording   Subject = "call.recording.uploaded"
	SubjectCallProviderEvt Subject = "call.provider.event"

	// Lead subjects.
	SubjectLeadCreated     Subject = "lead.created"
	SubjectLeadScored      Subject = "lead.scored"
	SubjectLeadHotDetected Subject = "lead.hot.detected"

	// Handoff subjects.
	SubjectHandoffRequested Subject = "handoff.requested"
	SubjectHandoffCompleted Subject = "handoff.completed"

	// WhatsApp subjects.
	SubjectWATemplateSent      Subject = "wa.template.sent"
	SubjectWATemplateDelivered Subject = "wa.template.delivered"
	SubjectWATemplateFailed    Subject = "wa.template.failed"
	SubjectWAMessageReceived   Subject = "wa.message.received"
	SubjectWAMessageSent       Subject = "wa.message.sent"
	SubjectWAOptOut            Subject = "wa.optout"
	SubjectWAStatusUpdated     Subject = "wa.status.updated"

	// Billing subjects.
	SubjectBillingUsage  Subject = "billing.usage"
	SubjectBillingCapHit Subject = "billing.cap.hit"

	// Campaign subjects.
	SubjectCampaignLaunched Subject = "campaign.launched"
	SubjectCampaignPaused   Subject = "campaign.paused"

	// Site-visit subjects.
	SubjectSiteVisitScheduled Subject = "site.visit.scheduled"
	SubjectSiteVisitCompleted Subject = "site.visit.completed"

	// Claim-control subjects.
	SubjectClaimViolation Subject = "claim.violation"

	// FreeSWITCH subjects.
	SubjectFreeSwitchChannelCreated     Subject = "freeswitch.channel.created"
	SubjectFreeSwitchChannelAnswered    Subject = "freeswitch.channel.answered"
	SubjectFreeSwitchChannelHangup      Subject = "freeswitch.channel.hangup"
	SubjectFreeSwitchRecordingStopped   Subject = "freeswitch.recording.stopped"
	SubjectFreeSwitchAudioStreamStarted Subject = "freeswitch.audio_stream.started"
	SubjectFreeSwitchAudioStreamStopped Subject = "freeswitch.audio_stream.stopped"
	SubjectFreeSwitchAudioStreamError   Subject = "freeswitch.audio_stream.error"
	SubjectFreeSwitchAudioStreamEvent   Subject = "freeswitch.audio_stream.event"
)

// StreamSpec describes a JetStream stream Capsy expects to exist.
type StreamSpec struct {
	Name     string
	Subjects []string
}

// DefaultStreams is the set of streams asserted by Publisher.New.
var DefaultStreams = []StreamSpec{
	{Name: "CAPSY_CALL", Subjects: []string{"call.>"}},
	{Name: "CAPSY_LEAD", Subjects: []string{"lead.>"}},
	{Name: "CAPSY_HANDOFF", Subjects: []string{"handoff.>"}},
	{Name: "CAPSY_WA", Subjects: []string{"wa.>"}},
	{Name: "CAPSY_BILLING", Subjects: []string{"billing.>"}},
	{Name: "CAPSY_CAMPAIGN", Subjects: []string{"campaign.>"}},
	{Name: "CAPSY_SITE", Subjects: []string{"site.>"}},
	{Name: "CAPSY_CLAIM", Subjects: []string{"claim.>"}},
	{Name: "CAPSY_FREESWITCH", Subjects: []string{"freeswitch.>"}},
}
