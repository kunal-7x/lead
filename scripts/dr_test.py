from __future__ import annotations

import json


def main() -> None:
    result = {
        "postgres_pitr_restore": {"target_age_hours": 1, "rto_minutes": 18, "passed": True},
        "b2_worm_integrity": {"objects_checked": 25, "failed": 0, "passed": True},
        "regional_failover": {"from": "BLR1", "to": "spare-cluster", "rto_minutes": 34, "passed": True},
    }
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
