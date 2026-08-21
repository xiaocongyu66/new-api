#!/usr/bin/env python3
"""Parse `go test -bench` output; fail if new engine > 2.5x legacy.

Input: go test output (multiple -count iterations).
Output: median ns/op each side, ratio, verdict. Exit 1 on regression.
"""
import re
import statistics
import sys

line_re = re.compile(
    r"^BenchmarkSensitive(Normal|LegacyNormal)-\d+\s+\d+\s+([\d.]+)\s+ns/op"
)

new_vals: list[float] = []
legacy_vals: list[float] = []

for line in sys.stdin:
    m = line_re.match(line.strip())
    if not m:
        continue
    (new_vals if m.group(1) == "Normal" else legacy_vals).append(float(m.group(2)))

if not new_vals or not legacy_vals:
    print("::error::bench output missing one side (new=%d legacy=%d)" % (len(new_vals), len(legacy_vals)))
    sys.exit(2)

def med(vs: list[float]) -> float:
    vs = sorted(vs)
    n = len(vs)
    return vs[n // 2] if n % 2 else (vs[n // 2 - 1] + vs[n // 2]) / 2

new_m, legacy_m = med(new_vals), med(legacy_vals)
ratio = new_m / legacy_m

print(f"new engine : {new_m:12.0f} ns/op  (median of {len(new_vals)} runs)")
print(f"legacy     : {legacy_m:12.0f} ns/op  (median of {len(legacy_vals)} runs)")
print(f"ratio      : {ratio:.2f}x")
ok = ratio <= 2.5
print("verdict    :", "OK (≤ 2.5x)" if ok else "FAIL (> 2.5x)")

with open("sensitive-bench.md", "w") as f:
    f.write(f"### Sensitive engine vs legacy\n\n")
    f.write(f"| | ns/op |\n|---|---:|\n")
    f.write(f"| new engine | {new_m:.0f} |\n")
    f.write(f"| legacy | {legacy_m:.0f} |\n")
    f.write(f"| **ratio** | **{ratio:.2f}x** |\n")
    f.write(f"\nVerdict: **{'PASS' if ok else 'FAIL'}** (limit 2.5x)\n")

if not ok:
    sys.exit(1)