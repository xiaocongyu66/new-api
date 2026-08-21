#!/usr/bin/env python3
"""Parse `go test -bench` output → fail if new engine > 2.5x legacy.

Usage: sensitive-bench-ratio.py [bench-output-file]
(reads stdin when no file argument given)
"""
import re
import statistics
import sys

line_re = re.compile(
    r"^BenchmarkSensitive(Normal|LegacyNormal)-\d+\s+\d+\s+([\d.]+)\s+ns/op"
)

new_vals: list[float] = []
legacy_vals: list[float] = []

if len(sys.argv) > 1:
    src = open(sys.argv[1], encoding="utf-8")
else:
    src = sys.stdin
for line in src:
    m = line_re.match(line.strip())
    if not m:
        continue
    (new_vals if m.group(1) == "Normal" else legacy_vals).append(float(m.group(2)))
if src is not sys.stdin:
    src.close()

if not new_vals or not legacy_vals:
    print(f"::error::benchmark output missing one side (new={len(new_vals)} legacy={len(legacy_vals)})")
    sys.exit(2)

new_m = statistics.median(new_vals)
legacy_m = statistics.median(legacy_vals)
if legacy_m <= 0:
    print(f"::error::legacy median ns/op is {legacy_m}; cannot compute ratio")
    sys.exit(2)

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

sys.exit(0 if ok else 1)