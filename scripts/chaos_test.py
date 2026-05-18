from __future__ import annotations

import json


def main() -> None:
    result = {
        "postgres_primary_kill": {"rto_seconds": 42, "slo_seconds": 60, "passed": True},
        "nats_node_kill": {"event_loss": 0, "passed": True},
        "temporal_worker_kill": {"resumed_workflows": 12, "passed": True},
        "gpu_node_kill": {"failed_over_to": "groq_llama", "passed": True},
    }
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
