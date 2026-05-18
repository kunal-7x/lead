# Production Preparation Checklist

Prepare these while the demo product gaps are being closed. Do not paste secrets into chat. Put real values in a local `.env.production.local` or a secrets manager when requested.

## Business Decisions

- Product display name.
- Final domain and subdomains:
  - `app.<domain>`
  - `api.<domain>`
  - `admin.<domain>`
  - `webhooks.<domain>` if separate.
- Support email.
- Admin/super-admin email.
- Default languages for first client.
- Default calling hours.
- Max call duration.
- Retry ladder.
- First client/tenant name.
- First project name, city, locality, price range, RERA number, possession, amenities, disclaimers.
- Sales team users and roles.

## Legal And Compliance

- TRAI/DLT principal entity registration.
- 140-series promotional number or approved compliant outbound route.
- Written consent/source policy for uploaded leads.
- Privacy policy URL.
- Terms of service URL.
- WhatsApp opt-out wording.
- Recording disclaimer wording.
- Data deletion/export process owner.
- RERA documents and approved project facts.
- GST details if invoicing clients.

## Infrastructure

- DigitalOcean account and API token.
- DOKS cluster or exact decision to use single-server staging first.
- Managed Postgres URL.
- Managed Redis URL.
- NATS JetStream endpoint.
- Temporal endpoint.
- ClickHouse endpoint.
- DO Spaces bucket and keys.
- Backblaze B2 bucket and keys for immutable/legal archive.
- Vault URL and bootstrap access.
- Container registry name.
- DNS managed in Cloudflare.
- TLS/cert-manager or Cloudflare tunnel plan.

## Telephony

At least one real path:

- Jio SIP trunk details:
  - SIP server.
  - Signal IP.
  - Media IP.
  - DID/140-series caller ID.
  - Server static IP to whitelist.
  - Concurrent channel limit.
- Airtel SIP fallback details if available.
- Plivo fallback:
  - Auth ID.
  - Auth token.
  - From number.
  - Webhook auth/signature settings.
- Human transfer numbers for sales team.
- Test numbers allowed for staging calls.

## WhatsApp

- Meta Business verified account.
- WABA ID.
- Phone Number ID.
- Permanent access token.
- Webhook verify token.
- App secret.
- Approved templates:
  - brochure follow-up.
  - site visit confirmation.
  - callback confirmation.
  - no-show recovery.
  - opt-out/help text.
- Brochure/price sheet/location map public asset URLs.

## AI Providers

- Sarvam API key for STT/TTS.
- Groq API key.
- OpenRouter API key.
- Optional OpenAI key.
- Optional Anthropic key.
- Optional Google AI key.
- Langfuse public key, secret key, and host.
- Model preference:
  - API-first with Sarvam/Groq.
  - self-hosted GPU later.
- If GPU later:
  - RunPod/Vast/DO GPU account.
  - HuggingFace token.
  - accepted model licenses.

## Test Data For First Real Staging Run

- 10 to 20 consented phone numbers owned by you/team/testers.
- Names and preferred languages.
- Expected scripted responses for validation.
- One approved project KB.
- One approved WhatsApp template set.
- One sales user for handoff.
- One manager user for escalation.

## Operational Readiness

- GitHub repository access and CI secrets.
- Deployment environment names: `staging`, `production`.
- Backup retention policy.
- Recording retention policy.
- On-call/alert receiver.
- Error monitoring destination.
- Runbook owner.
- Client onboarding checklist owner.

## What Not To Prepare Yet

- 500 real leads.
- Production client credentials.
- High-volume campaign budget.
- GPU server.

Those come after the full fake/demo product journey passes and a small staging live test succeeds.

