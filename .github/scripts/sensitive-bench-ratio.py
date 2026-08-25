#!/usr/bin/env python3
"""Parse `go test -bench` output → fail if new engine normal-pool latency
exceeds the absolute budget.

Gate: median of BenchmarkSensitiveNormal ≤ 400ms per op (3000-row normal pool).
The old "new vs legacy ratio" gate was retired: the legacy baseline is pure
dict-AC whose cost is proportional to dictionary size, so trimming the
production dictionary (3773 → 2 words) collapsed the denominator and made the
ratio meaningless while the new engine actually got faster. An absolute
latency budget guards the real invariant: normal requests must stay cheap.

Usage: sensitive-bench-ratio.py [bench-output-file]
(reads stdin when no file argument given)
"""
import re
import statistics
import sys
from collections.abc import Iterator

BUDGET_NS = 400_000_000  # 400ms per op over the 3000-row normal pool

line_re = re.compile(
    r"^BenchmarkSensitiveNormal-\d+\s+\d+\s+([\d.]+)\s+ns/op"
)

new_vals: list[float] = []

def _iter_lines() -> Iterator[str]:
    if len(sys.argv) > 1:
        with open(sys.argv[1], encoding="utf-8") as f:
            yield from f
    else:
        yield from sys.stdin


for line in _iter_lines():
    m = line_re.match(line.strip())
    if m:
        new_vals.append(float(m.group(1)))

if not new_vals:
    print("::error::benchmark output has no BenchmarkSensitiveNormal rows")
    sys.exit(2)

new_m = statistics.median(new_vals)
ok = new_m <= BUDGET_NS

print(f"new engine : {new_m:12.0f} ns/op  (median of {len(new_vals)} runs)")
print(f"budget     : {BUDGET_NS:12.0f} ns/op")
print("verdict    :", "OK" if ok else "FAIL (over budget)")

with open("sensitive-bench.md", "w") as f:
    f.write("### Sensitive engine normal-pool latency\n\n")
    f.write("| | ns/op |\n|---|---:|\n")
    f.write(f"| new engine | {new_m:.0f} |\n")
    f.write(f"| budget | {BUDGET_NS:.0f} |\n")
    f.write(f"\nVerdict: **{'PASS' if ok else 'FAIL'}** (budget {BUDGET_NS/1e6:.0f}ms)\n")

sys.exit(0 if ok else 1)
