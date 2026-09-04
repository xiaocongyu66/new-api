#!/usr/bin/env python3
"""Synthetic data self-tests for lib_resources."""
from __future__ import annotations

import sys
import tempfile
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib_resources import sample_once, ResourceSampler, aggregate_windows, NOT_AVAILABLE, read_k8s_pod_node_info


def test_sample_once_structure() -> bool:
    """sample_once() returns dict with all four top-level categories."""
    s = sample_once()
    assert isinstance(s, dict), "sample_once must return dict"
    assert "ts" in s and isinstance(s["ts"], float), "missing ts float"
    for cat in ("cpu", "mem", "disk", "net"):
        assert cat in s, f"missing category {cat}"
        assert isinstance(s[cat], dict), f"{cat} must be dict"
    assert "unavailable_reasons" in s and isinstance(s["unavailable_reasons"], dict), "missing unavailable_reasons"
    # cpu keys
    for k in ("user", "system", "iowait", "steal", "load1", "load5", "load15", "runqueue"):
        assert k in s["cpu"], f"cpu missing {k}"
    # mem keys
    for k in ("rss", "available", "cgroup_usage", "cgroup_limit"):
        assert k in s["mem"], f"mem missing {k}"
    # disk keys
    for k in ("used", "free", "inodes", "read_bps", "write_bps", "iops", "util", "await", "queue", "errors"):
        assert k in s["disk"], f"disk missing {k}"
    # net keys
    for k in ("rx", "tx", "retransmit", "drop", "error", "conns"):
        assert k in s["net"], f"net missing {k}"
    return True


def test_aggregate_windows_bucketing() -> bool:
    """aggregate_windows correctly buckets into 10s windows with exact avg/max."""
    base = 1000.0
    rows = []
    # 25 samples: 0-12s (window 0), 10-22s (window 10), 20-24.5s (window 20)
    # Window 0 (1000-1010): samples at 1000.0, 1000.5, ..., 1009.5 => 20 samples
    # Window 10 (1010-1020): samples at 1010.0, 1010.5, ..., 1019.5 => 20 samples? wait 25 total
    # Let's do: 25 samples * 0.5s = 12.5s span from 1000 to 1012.5
    # Windows: 1000-1010 (indices 0-19 = 20 samples), 1010-1020 (indices 20-24 = 5 samples)
    for i in range(25):
        rows.append({
            "ts": base + i * 0.5,
            "cpu": {"user": 10.0 + i, "system": 5.0, "iowait": 1.0, "steal": 0.0, "load1": 1.0, "load5": 1.0, "load15": 1.0, "runqueue": 2},
            "mem": {"rss": 100000000 + i * 1000, "available": 5000000000, "cgroup_usage": NOT_AVAILABLE, "cgroup_limit": NOT_AVAILABLE},
            "disk": {"used": 10000000000, "free": 50000000000, "inodes": 100000, "read_bps": 1000000, "write_bps": 500000, "iops": 100, "util": 10.0, "await": 2.0, "queue": 0.5, "errors": NOT_AVAILABLE},
            "net": {"rx": 100000, "tx": 50000, "retransmit": 0, "drop": 0, "error": 0, "conns": 10},
            "unavailable_reasons": {}
        })

    windows = aggregate_windows(rows, window_s=10)
    assert len(windows) == 2, f"expected 2 windows, got {len(windows)}"

    # Window 0: ts 1000-1009.5 (20 samples), user values 10.0 to 29.0 step 1
    w0 = windows[0]
    assert w0["window_start"] == 1000
    assert w0["window_end"] == 1010
    assert w0["samples"] == 20
    # avg of 10.0..29.0 = (10+29)*20/2/20 = 19.5
    assert w0["cpu"]["avg"]["user"] == 19.5, f"w0 cpu avg user={w0['cpu']['avg']['user']}"
    assert w0["cpu"]["max"]["user"] == 29.0, f"w0 cpu max user={w0['cpu']['max']['user']}"

    # Window 10: ts 1010-1012.5 (5 samples), user values 30.0 to 34.0
    w1 = windows[1]
    assert w1["window_start"] == 1010
    assert w1["window_end"] == 1020
    assert w1["samples"] == 5
    # avg of 30..34 = 32.0
    assert w1["cpu"]["avg"]["user"] == 32.0, f"w1 cpu avg user={w1['cpu']['avg']['user']}"
    assert w1["cpu"]["max"]["user"] == 34.0, f"w1 cpu max user={w1['cpu']['max']['user']}"

    # mem rss avg window 0: 100000000 + (0+19)*1000/2 = 100000000 + 9500 = 100009500
    expected_rss_avg_0 = 100000000 + 9500
    assert w0["mem"]["avg"]["rss"] == expected_rss_avg_0, f"w0 mem avg rss={w0['mem']['avg']['rss']}"
    return True


def test_aggregate_windows_not_available_excluded() -> bool:
    """NOT_AVAILABLE values are skipped in aggregation, not poisoning avg/max."""
    base = 2000.0
    rows = []
    # 10 samples in one window, 3 have NOT_AVAILABLE for cgroup_usage
    for i in range(10):
        cg = NOT_AVAILABLE if i % 3 == 0 else 1000 + i * 100
        rows.append({
            "ts": base + i,
            "cpu": {"user": 10.0, "system": 5.0, "iowait": 1.0, "steal": 0.0, "load1": 1.0, "load5": 1.0, "load15": 1.0, "runqueue": 2},
            "mem": {"rss": 100000000, "available": 5000000000, "cgroup_usage": cg, "cgroup_limit": NOT_AVAILABLE},
            "disk": {"used": 10000000000, "free": 50000000000, "inodes": 100000, "read_bps": 1000000, "write_bps": 500000, "iops": 100, "util": 10.0, "await": 2.0, "queue": 0.5, "errors": NOT_AVAILABLE},
            "net": {"rx": 100000, "tx": 50000, "retransmit": 0, "drop": 0, "error": 0, "conns": 10},
            "unavailable_reasons": {}
        })

    windows = aggregate_windows(rows, window_s=10)
    assert len(windows) == 1
    w = windows[0]
    # cgroup_usage available for i=1,2,4,5,7,8 => values 1100,1200,1400,1500,1700,1800
    # avg = (1100+1200+1400+1500+1700+1800)/6 = 8700/6 = 1450
    assert w["mem"]["avg"]["cgroup_usage"] == 1450.0, f"cgroup_usage avg={w['mem']['avg']['cgroup_usage']}"
    assert w["mem"]["max"]["cgroup_usage"] == 1800.0, f"cgroup_usage max={w['mem']['max']['cgroup_usage']}"
    # cgroup_limit all NOT_AVAILABLE -> result NOT_AVAILABLE
    assert w["mem"]["avg"]["cgroup_limit"] == NOT_AVAILABLE
    assert w["mem"]["max"]["cgroup_limit"] == NOT_AVAILABLE
    return True


def test_resource_sampler_short_run() -> bool:
    """ResourceSampler writes ndjson lines for short run."""
    with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".ndjson") as f:
        tmp = Path(f.name)
    try:
        sampler = ResourceSampler(interval=0.1, out_path=tmp)
        sampler.start()
        time.sleep(0.35)  # ~3 intervals
        sampler.stop()

        with open(tmp) as f:
            lines = [line.strip() for line in f if line.strip()]
        assert len(lines) >= 2, f"expected >=2 lines, got {len(lines)}"
        # Each line should be valid JSON with required structure
        import json
        for line in lines:
            obj = json.loads(line)
            assert "ts" in obj
            assert "cpu" in obj
            assert "mem" in obj
            assert "disk" in obj
            assert "net" in obj
        return True
    finally:
        if tmp.exists():
            tmp.unlink()


def test_aggregate_windows_empty_input() -> bool:
    """aggregate_windows handles empty input."""
    result = aggregate_windows([])
    assert result == [], "empty input should return empty list"
    return True


def test_aggregate_windows_single_sample() -> bool:
    """aggregate_windows handles single sample."""
    rows = [{
        "ts": 1000.0,
        "cpu": {"user": 10.0, "system": 5.0, "iowait": 1.0, "steal": 0.0, "load1": 1.0, "load5": 1.0, "load15": 1.0, "runqueue": 2},
        "mem": {"rss": 100000000, "available": 5000000000, "cgroup_usage": NOT_AVAILABLE, "cgroup_limit": NOT_AVAILABLE},
        "disk": {"used": 10000000000, "free": 50000000000, "inodes": 100000, "read_bps": 1000000, "write_bps": 500000, "iops": 100, "util": 10.0, "await": 2.0, "queue": 0.5, "errors": NOT_AVAILABLE},
        "net": {"rx": 100000, "tx": 50000, "retransmit": 0, "drop": 0, "error": 0, "conns": 10},
        "unavailable_reasons": {}
    }]
    windows = aggregate_windows(rows, window_s=10)
    assert len(windows) == 1
    w = windows[0]
    assert w["samples"] == 1
    assert w["cpu"]["avg"]["user"] == 10.0
    assert w["cpu"]["max"]["user"] == 10.0
    return True


def test_aggregate_windows_boundary_exact() -> bool:
    """Samples exactly at window boundary go to the next window."""
    # Sample at ts=1010.0 should go to window 1010-1020, not 1000-1010
    rows = [
        {"ts": 1000.0, "cpu": {"user": 1.0, "system": 0, "iowait": 0, "steal": 0, "load1": 0, "load5": 0, "load15": 0, "runqueue": 0},
         "mem": {"rss": 0, "available": 0, "cgroup_usage": NOT_AVAILABLE, "cgroup_limit": NOT_AVAILABLE},
         "disk": {"used": 0, "free": 0, "inodes": 0, "read_bps": 0, "write_bps": 0, "iops": 0, "util": 0, "await": 0, "queue": 0, "errors": NOT_AVAILABLE},
         "net": {"rx": 0, "tx": 0, "retransmit": 0, "drop": 0, "error": 0, "conns": 0},
         "unavailable_reasons": {}},
        {"ts": 1010.0, "cpu": {"user": 2.0, "system": 0, "iowait": 0, "steal": 0, "load1": 0, "load5": 0, "load15": 0, "runqueue": 0},
         "mem": {"rss": 0, "available": 0, "cgroup_usage": NOT_AVAILABLE, "cgroup_limit": NOT_AVAILABLE},
         "disk": {"used": 0, "free": 0, "inodes": 0, "read_bps": 0, "write_bps": 0, "iops": 0, "util": 0, "await": 0, "queue": 0, "errors": NOT_AVAILABLE},
         "net": {"rx": 0, "tx": 0, "retransmit": 0, "drop": 0, "error": 0, "conns": 0},
         "unavailable_reasons": {}},
    ]
    windows = aggregate_windows(rows, window_s=10)
    assert len(windows) == 2
    assert windows[0]["window_start"] == 1000
    assert windows[0]["samples"] == 1
    assert windows[0]["cpu"]["avg"]["user"] == 1.0
    assert windows[1]["window_start"] == 1010
    assert windows[1]["samples"] == 1
    assert windows[1]["cpu"]["avg"]["user"] == 2.0
    return True


def test_aggregate_windows_folds_go_runtime() -> bool:
    """aggregate_windows folds go_runtime dict (avg/max), skipping NOT_AVAILABLE."""
    base = 3000.0
    rows = []
    go_vals = [
        {"heap_alloc": 100, "heap_sys": 200, "heap_inuse": 150, "heap_objects": 50,
         "next_gc": 400, "num_gc": 10, "gc_pause_total_ns": 5000, "sys_total": 300},
        NOT_AVAILABLE,  # pprof unreachable for this sample
        {"heap_alloc": 300, "heap_sys": 200, "heap_inuse": 250, "heap_objects": 70,
         "next_gc": 600, "num_gc": 25, "gc_pause_total_ns": 8000, "sys_total": 300},
    ]
    for i, gr in enumerate(go_vals):
        rows.append({
            "ts": base + i,
            "cpu": {"user": 10.0, "system": 5.0, "iowait": 1.0, "steal": 0.0, "load1": 1.0, "load5": 1.0, "load15": 1.0, "runqueue": 2},
            "mem": {"rss": 100000000, "available": 5000000000, "cgroup_usage": NOT_AVAILABLE, "cgroup_limit": NOT_AVAILABLE},
            "disk": {"used": 10000000000, "free": 50000000000, "inodes": 100000, "read_bps": 1000000, "write_bps": 500000, "iops": 100, "util": 10.0, "await": 2.0, "queue": 0.5, "errors": NOT_AVAILABLE},
            "net": {"rx": 100000, "tx": 50000, "retransmit": 0, "drop": 0, "error": 0, "conns": 10},
            "go_runtime": gr,
            "unavailable_reasons": {},
        })

    windows = aggregate_windows(rows, window_s=10)
    assert len(windows) == 1
    w = windows[0]
    # Only the 2 numeric samples count: heap_alloc avg=(100+300)/2=200, max=300
    assert w["go_runtime"]["avg"]["heap_alloc"] == 200.0, f"go avg heap_alloc={w['go_runtime']['avg']['heap_alloc']}"
    assert w["go_runtime"]["max"]["heap_alloc"] == 300.0, f"go max heap_alloc={w['go_runtime']['max']['heap_alloc']}"
    # num_gc avg=(10+25)/2=17.5, max=25
    assert w["go_runtime"]["avg"]["num_gc"] == 17.5, f"go avg num_gc={w['go_runtime']['avg']['num_gc']}"
    assert w["go_runtime"]["max"]["num_gc"] == 25.0, f"go max num_gc={w['go_runtime']['max']['num_gc']}"
    return True


def test_read_k8s_pod_node_info_local_returns_none() -> bool:
    """read_k8s_pod_node_info() returns (None, reason) on a non-k8s host."""
    info, reason = read_k8s_pod_node_info()
    assert info is None, f"expected None outside k8s, got {info!r}"
    assert isinstance(reason, str), f"reason should be str, got {type(reason)}"
    assert "not running inside Kubernetes" in reason, f"reason text mismatch: {reason}"
    return True

def run_all() -> int:
    tests = [
        ("sample_once_structure", test_sample_once_structure),
        ("aggregate_windows_bucketing", test_aggregate_windows_bucketing),
        ("aggregate_windows_not_available_excluded", test_aggregate_windows_not_available_excluded),
        ("resource_sampler_short_run", test_resource_sampler_short_run),
        ("aggregate_windows_empty_input", test_aggregate_windows_empty_input),
        ("aggregate_windows_single_sample", test_aggregate_windows_single_sample),
        ("aggregate_windows_boundary_exact", test_aggregate_windows_boundary_exact),
        ("aggregate_windows_folds_go_runtime", test_aggregate_windows_folds_go_runtime),
        ("read_k8s_pod_node_info_local_returns_none", test_read_k8s_pod_node_info_local_returns_none),
    ]
    failed = 0
    for name, fn in tests:
        try:
            if fn():
                print(f"✓ {name}")
            else:
                print(f"✗ {name}: returned False")
                failed += 1
        except AssertionError as e:
            print(f"✗ {name}: {e}")
            failed += 1
        except Exception as e:
            print(f"✗ {name}: unexpected error: {e}")
            failed += 1
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(run_all())