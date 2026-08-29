"""Resolve the per-scenario mock MOCK_FORCE_MODE list from lib_stats.

Used by the GitHub Actions workflow to choose which MOCK_FORCE_MODE each
in-cluster mock pod should start with. The runner's preflight /healthz
check enforces that the live mock's force_mode matches the scenario's
declared injection plan, so the workflow must read the plan from the
authoritative source (lib_stats.scenario_targets) and start the mocks
accordingly.

Returns a JSON object on stdout:
  {"instances": <int>, "modes": [<str>, ...]}

instances is the number of mock pods to start (>= the number of routes
expected by the scenario). modes[i] is the MOCK_FORCE_MODE for pod i.
"""
from __future__ import annotations

import json
import os
import sys


def main() -> int:
    sys.path.insert(0, os.path.join(os.path.dirname(__file__)))
    from lib_stats import scenario_targets  # noqa: E402

    sc = os.environ.get("SCENARIO", "").strip()
    if not sc:
        print(json.dumps({"instances": 2, "modes": ["ok", "ok"]}))
        return 0
    targets = scenario_targets().get(sc, {})
    injection = targets.get("injection", {}) or {}
    # Prefer labels A/B; fall back to any declared keys, then default.
    if "A" in injection and "B" in injection:
        labels = ["A", "B"]
    elif injection:
        labels = list(injection.keys())
    else:
        labels = ["A", "B"]
    modes = [str(injection.get(lbl, "ok")) for lbl in labels] or ["ok", "ok"]
    instances = max(len(modes), 2)
    print(json.dumps({"instances": instances, "modes": modes}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
