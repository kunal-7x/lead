# Port Map

| Range       | Purpose                              |
|-------------|--------------------------------------|
| 8080        | BFF (Backend-for-Frontend)           |
| 8101–8120   | Control-plane Go services            |
| 8201–8210   | AI-plane Python services             |
| 9000+       | Infra (NATS, Temporal, LiveKit, etc.)|

## Allocations

### Control plane (Go) 8101–8120
| Port | Service              |
|------|----------------------|
| 8101 | auth-service         |
| 8102 | tenant-service       |
| 8103 | lead-service         |
| 8104 | campaign-service     |
| 8105 | telephony-gateway    |
| 8106 | whatsapp-adapter     |
| 8107 | scheduler            |
| 8108 | billing-service      |
| 8109 | analytics-ingest     |
| 8110 | webhook-router       |
| 8112 | handoff              |
| 8113 | notification         |
| 8114 | site-visit           |
| 8115 | billing-meter        |
| 8116 | analytics-sink       |
| 8117 | model-config         |
| 8118 | internal-admin-api   |

### AI plane (Python) 8201–8210
| Port | Service              |
|------|----------------------|
| 8201 | conversation-orchestrator |
| 8202 | asr-service          |
| 8203 | tts-service          |
| 8204 | llm-router           |
| 8205 | rag-service          |
| 8206 | summarizer           |
| 8207 | qualification-agent  |

### Apps
| Port | App                  |
|------|----------------------|
| 3000 | dashboard (Next.js)  |
| 3001 | internal-admin-ui    |

### Infra 9000+
| Port  | Service          |
|-------|------------------|
| 4222  | NATS             |
| 7233  | Temporal         |
| 7880  | LiveKit          |
| 8200  | Vault            |
| 8123  | ClickHouse       |
| 3100  | Loki             |
| 3200  | Tempo            |
| 9090  | Prometheus       |
| 3001  | Grafana          |
| 9000  | GlitchTip        |
| 3030  | Langfuse         |
