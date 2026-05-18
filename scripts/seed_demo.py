from __future__ import annotations

import argparse
import hashlib
import json
import random
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
OUT_DIR = ROOT / ".demo"
OUT_FILE = OUT_DIR / "demo_seed.json"
SEED = 23023


def build_demo_data() -> dict[str, Any]:
    rng = random.Random(SEED)
    salespeople = [
        {"id": "sales-demo-1", "name": "Riya Sharma"},
        {"id": "sales-demo-2", "name": "Kabir Mehta"},
        {"id": "sales-demo-3", "name": "Anika Rao"},
        {"id": "sales-demo-4", "name": "Dev Malhotra"},
    ]
    projects = [
        {"id": "project-skyline", "name": "Skyline Residency", "kb_version": "kb-demo-approved-1"},
        {"id": "project-lakeview", "name": "Lakeview Towers", "kb_version": "kb-demo-approved-1"},
    ]
    statuses = ["hot", "warm", "cold", "call_later", "not_interested"]
    status_weights = [0.16, 0.34, 0.22, 0.2, 0.08]
    sources = ["99acres", "magicbricks", "facebook", "google", "walkin"]

    leads = []
    for idx in range(500):
        status = rng.choices(statuses, weights=status_weights, k=1)[0]
        project = rng.choice(projects)
        salesperson = rng.choice(salespeople)
        budget_lakh = rng.choice([45, 55, 65, 75, 85, 95, 110, 130])
        leads.append(
            {
                "id": f"lead-demo-{idx + 1:03d}",
                "tenant_id": "tenant-demo",
                "project_id": project["id"],
                "name": f"Demo Lead {idx + 1:03d}",
                "phone": f"+9198{idx + 10000000:08d}",
                "source": rng.choice(sources),
                "status": status,
                "budget_lakh": budget_lakh,
                "property_type": rng.choice(["1BHK", "2BHK", "3BHK"]),
                "assigned_to": salesperson["id"],
                "demo": True,
            }
        )

    calls = [
        {
            "id": f"call-demo-{idx + 1:03d}",
            "lead_id": leads[idx]["id"],
            "tenant_id": "tenant-demo",
            "provider": "mock-plivo",
            "duration_seconds": rng.randint(35, 260),
            "outcome": rng.choice(["qualified", "callback", "site_visit", "not_interested"]),
            "external_api_calls": 0,
        }
        for idx in range(50)
    ]
    conversations = [
        {
            "id": f"wa-demo-{idx + 1:03d}",
            "lead_id": leads[idx]["id"],
            "tenant_id": "tenant-demo",
            "provider": "mock-meta-wa",
            "last_reply": rng.choice(["Interested", "Send brochure", "Book visit", "Call later"]),
            "delivered": True,
            "external_api_calls": 0,
        }
        for idx in range(30)
    ]
    site_visits = [
        {
            "id": f"visit-demo-{idx + 1:03d}",
            "lead_id": leads[idx]["id"],
            "tenant_id": "tenant-demo",
            "project_id": leads[idx]["project_id"],
            "status": rng.choice(["booked", "confirmed", "completed"]),
            "assigned_to": leads[idx]["assigned_to"],
        }
        for idx in range(10)
    ]
    costs = {
        "tenant_id": "tenant-demo",
        "call_seconds": sum(call["duration_seconds"] for call in calls),
        "wa_messages": len(conversations),
        "llm_calls": len(calls) * 3,
        "total_inr": 0.0,
        "demo": True,
    }
    data = {
        "version": "phase-23-demo-v1",
        "seed": SEED,
        "tenant": {
            "id": "tenant-demo",
            "name": "Capsy Demo Tenant",
            "demo": True,
            "domain": "demo.axcrio.com",
            "blocked_real_outbound": True,
        },
        "projects": projects,
        "knowledge_base": {
            "id": "kb-demo-approved-1",
            "status": "approved",
            "chunks": [
                "Skyline Residency has 2BHK and 3BHK inventory near the metro.",
                "Possession dates and loan eligibility must be verified by sales.",
                "Site visits are available from 10:00 to 18:00.",
            ],
        },
        "salespeople": salespeople,
        "leads": leads,
        "calls": calls,
        "whatsapp_conversations": conversations,
        "site_visits": site_visits,
        "cost_summaries": costs,
        "mock_providers": {
            "telephony": "mock-plivo",
            "whatsapp": "mock-meta-wa",
            "stt": "mock-sarvam",
            "llm": "mock-groq",
            "openrouter": "mock-openrouter",
            "tts": "mock-elevenlabs",
        },
    }
    data["checksum"] = checksum(data)
    return data


def checksum(data: dict[str, Any]) -> str:
    clone = {key: value for key, value in data.items() if key != "checksum"}
    payload = json.dumps(clone, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()[:16]


def summary(data: dict[str, Any]) -> dict[str, Any]:
    return {
        "tenant_id": data["tenant"]["id"],
        "demo": data["tenant"]["demo"],
        "leads": len(data["leads"]),
        "calls": len(data["calls"]),
        "whatsapp_conversations": len(data["whatsapp_conversations"]),
        "site_visits": len(data["site_visits"]),
        "checksum": data["checksum"],
    }


def write_data(data: dict[str, Any]) -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    OUT_FILE.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def verify() -> None:
    if not OUT_FILE.exists():
        raise SystemExit(f"missing demo seed file: {OUT_FILE}")
    current = json.loads(OUT_FILE.read_text(encoding="utf-8"))
    expected = build_demo_data()
    if summary(current) != summary(expected):
        raise SystemExit(f"demo seed verification failed: {summary(current)} != {summary(expected)}")
    print(json.dumps({"verified": True, **summary(current)}, sort_keys=True))


def main() -> None:
    parser = argparse.ArgumentParser(description="Seed deterministic Capsy demo data")
    parser.add_argument("--dry-run", action="store_true", help="print summary without writing")
    parser.add_argument("--reset", action="store_true", help="wipe and re-seed demo data")
    parser.add_argument("--verify", action="store_true", help="verify existing seed output")
    args = parser.parse_args()

    if args.verify and not args.reset:
        verify()
        return

    if args.reset and OUT_FILE.exists():
        OUT_FILE.unlink()

    data = build_demo_data()
    if args.dry_run:
        print(json.dumps({"dry_run": True, **summary(data)}, sort_keys=True))
        return

    write_data(data)
    if args.verify:
        verify()
        return
    print(json.dumps({"seeded": True, **summary(data)}, sort_keys=True))


if __name__ == "__main__":
    main()
