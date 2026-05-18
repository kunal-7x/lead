from __future__ import annotations

import json
from dataclasses import asdict, dataclass
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
FIXTURE = Path(__file__).with_name("fixtures") / "ten_leads.json"
REPORT_PATH = ROOT / ".demo" / "demo_flow_report.json"


@dataclass(frozen=True)
class GapCheck:
    key: str
    title: str
    status: str
    evidence: str
    next_step: str


def _read(relative: str) -> str:
    path = ROOT / relative
    if not path.exists():
        return ""
    return path.read_text(encoding="utf-8", errors="replace")


def _exists(relative: str) -> bool:
    return (ROOT / relative).exists()


def _status(condition: bool, when_true: str = "pass", when_false: str = "missing") -> str:
    return when_true if condition else when_false


def _load_leads() -> list[dict[str, str]]:
    return json.loads(FIXTURE.read_text(encoding="utf-8"))


def build_gap_report() -> dict:
    bff_server = _read("services/bff/internal/server/server.go")
    lead_import_service = _read("services/lead-import/internal/service/service.go")
    campaign_handler = _read("services/campaign/internal/handler/handler.go")
    scheduler_handler = _read("services/scheduler/internal/handler/handler.go")
    telephony_handler = _read("services/telephony-adapter/internal/handler/handler.go")
    voice_app = _read("services/voice-agent-worker/voice_agent/app.py")
    messages_page = _read("apps/dashboard/src/app/(protected)/messages/page.tsx")

    checks = [
        GapCheck(
            key="fixture.ten_leads",
            title="Deterministic 10-lead fixture",
            status=_status(len(_load_leads()) == 10),
            evidence=f"{len(_load_leads())} fixture leads",
            next_step="Use this fixture as the shared demo acceptance dataset.",
        ),
        GapCheck(
            key="dashboard.bff.lead_import_proxy",
            title="Dashboard lead import routes are proxied through BFF",
            status=_status("/v1/import/preview" in bff_server and "/v1/leads" in bff_server),
            evidence="BFF route table checked for lead import/list/detail routes.",
            next_step="Proxy lead-import service routes through BFF with tenant header propagation.",
        ),
        GapCheck(
            key="lead_import.service_routes",
            title="Lead import service exposes import and lead routes",
            status=_status("/v1/import/jobs" in lead_import_service and "/v1/leads" in lead_import_service),
            evidence="lead-import service route table includes import job and lead endpoints.",
            next_step="Wire these routes through BFF and shared demo state.",
        ),
        GapCheck(
            key="dashboard.campaign_ui",
            title="Client dashboard has campaign launch UI",
            status=_status(
                _exists("apps/dashboard/src/app/(protected)/campaigns/page.tsx")
                and "/v1/campaigns" in _read("apps/dashboard/src/app/(protected)/campaigns/page.tsx")
            ),
            evidence="Checked for protected dashboard campaigns route.",
            next_step="Add campaign list/create/attach/launch/health UI.",
        ),
        GapCheck(
            key="campaign.service_routes",
            title="Campaign service exposes launch and attach APIs",
            status=_status(
                "/v1/campaigns" in campaign_handler
                and "/{id}/leads" in campaign_handler
                and "/{id}/launch" in campaign_handler
            ),
            evidence="campaign service handler checked for attach and launch routes.",
            next_step="Connect campaign launch to scheduler demo orchestration.",
        ),
        GapCheck(
            key="scheduler.to_telephony",
            title="Scheduler starts demo telephony calls",
            status=_status(
                "/v1/scheduler/demo-dispatch" in scheduler_handler
                and "/v1/calls" in scheduler_handler
                and "telephony" in scheduler_handler.lower()
            ),
            evidence="scheduler handler checked for demo dispatch endpoint and telephony call command.",
            next_step="Add demo scheduler flow: launched campaign -> pick lead -> POST call command.",
        ),
        GapCheck(
            key="telephony.call_command_api",
            title="Telephony exposes product-level call command API",
            status=_status("/v1/calls" in telephony_handler),
            evidence="telephony handler route table checked for POST /v1/calls.",
            next_step="Add POST /v1/calls for scheduler/demo call creation.",
        ),
        GapCheck(
            key="voice.runtime_persistence",
            title="Voice worker persists events and turns outside tests",
            status=_status(
                "DemoEventPublisher" in voice_app
                and "DemoTurnStore" in voice_app
                and "FakePublisher()" not in voice_app
                and "FakeTurnStore()" not in voice_app
            ),
            evidence="voice WebSocket handler checked for demo runtime publisher and turn store.",
            next_step="Use demo event publisher and demo turn store in demo mode.",
        ),
        GapCheck(
            key="whatsapp.dashboard_inbox_api",
            title="WhatsApp inbox is API-backed",
            status=_status(
                "const threads = [" not in messages_page
                and "/v1/whatsapp/threads" in messages_page
                and "/v1/whatsapp/messages" in messages_page
            ),
            evidence="messages page uses WhatsApp thread/message APIs instead of local static arrays.",
            next_step="Extend WhatsApp replies into RAG/brain responses and lead timeline updates.",
        ),
        GapCheck(
            key="demo.full_e2e_command",
            title="One command proves the full 10-lead journey",
            status="partial",
            evidence="This harness exists and reports product gaps; later phases must convert gaps to pass.",
            next_step="Extend this suite until required demo flow artifacts all pass.",
        ),
    ]

    statuses = {status: sum(1 for check in checks if check.status == status) for status in ["pass", "partial", "missing"]}
    return {
        "scenario": "demo_10_lead_product_flow",
        "side_effects": "none",
        "fixture": str(FIXTURE.relative_to(ROOT)),
        "statuses": statuses,
        "checks": [asdict(check) for check in checks],
    }


def test_demo_fixture_is_deterministic_and_safe() -> None:
    leads = _load_leads()
    assert len(leads) == 10
    assert len({lead["phone"] for lead in leads}) == 10
    assert {lead["expected_outcome"] for lead in leads} >= {
        "opt_out",
        "site_visit",
        "hot_handoff",
        "callback",
        "no_answer_retry",
    }


def test_demo_flow_gap_report_is_structured() -> None:
    report = build_gap_report()
    REPORT_PATH.parent.mkdir(parents=True, exist_ok=True)
    REPORT_PATH.write_text(json.dumps(report, indent=2, sort_keys=True), encoding="utf-8")

    assert report["scenario"] == "demo_10_lead_product_flow"
    assert report["side_effects"] == "none"
    assert report["checks"]
    assert all(check["status"] in {"pass", "partial", "missing"} for check in report["checks"])
    assert report["statuses"]["missing"] == 0
