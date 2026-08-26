#!/usr/bin/env python3
"""Resource sampling from Linux procfs/sysfs (stdlib only)."""
from __future__ import annotations

import os
import time
import threading
import json
from pathlib import Path
from typing import Any
from dataclasses import dataclass, field


NOT_AVAILABLE = "NOT_AVAILABLE"


def _read_file(path: str) -> str | None:
    try:
        with open(path, "r") as f:
            return f.read()
    except (OSError, FileNotFoundError, PermissionError):
        return None


def _parse_proc_stat(content: str) -> dict[str, int] | None:
    """Parse /proc/stat first line (cpu aggregate). Returns dict with user, nice, system, idle, iowait, irq, softirq, steal, guest, guest_nice."""
    lines = content.strip().split("\n")
    if not lines:
        return None
    parts = lines[0].split()
    if len(parts) < 11 or parts[0] != "cpu":
        return None
    return {
        "user": int(parts[1]),
        "nice": int(parts[2]),
        "system": int(parts[3]),
        "idle": int(parts[4]),
        "iowait": int(parts[5]),
        "irq": int(parts[6]),
        "softirq": int(parts[7]),
        "steal": int(parts[8]),
        "guest": int(parts[9]),
        "guest_nice": int(parts[10]),
    }


def _parse_loadavg(content: str) -> dict[str, float] | None:
    """Parse /proc/loadavg. Returns 1m, 5m, 15m, run_queue, last_pid."""
    parts = content.strip().split()
    if len(parts) < 5:
        return None
    run_queue_part = parts[3].split("/")
    return {
        "load1": float(parts[0]),
        "load5": float(parts[1]),
        "load15": float(parts[2]),
        "runqueue": int(run_queue_part[0]) if run_queue_part else 0,
        "last_pid": int(parts[4]),
    }


def _parse_meminfo(content: str) -> dict[str, int]:
    """Parse /proc/meminfo. Returns key->value in kB."""
    result = {}
    for line in content.strip().split("\n"):
        if ":" in line:
            key, val = line.split(":", 1)
            val = val.strip()
            if val.endswith(" kB"):
                result[key] = int(val[:-3]) * 1024
    return result


def _parse_self_status(content: str) -> dict[str, int]:
    """Parse /proc/self/status. Returns VmRSS in bytes."""
    result = {}
    for line in content.strip().split("\n"):
        if ":" in line:
            key, val = line.split(":", 1)
            val = val.strip()
            if val.endswith(" kB"):
                result[key] = int(val[:-3]) * 1024
    return result


def _parse_diskstats(content: str) -> dict[str, dict[str, int]]:
    """Parse /proc/diskstats. Returns dict device->counters."""
    result = {}
    for line in content.strip().split("\n"):
        parts = line.split()
        if len(parts) < 14:
            continue
        # major minor name reads reads_merged sectors_read ms_read writes writes_merged sectors_written ms_write ios_in_progress ms_io weighted_ms_io
        name = parts[2]
        result[name] = {
            "reads": int(parts[3]),
            "reads_merged": int(parts[4]),
            "sectors_read": int(parts[5]),
            "ms_read": int(parts[6]),
            "writes": int(parts[7]),
            "writes_merged": int(parts[8]),
            "sectors_written": int(parts[9]),
            "ms_write": int(parts[10]),
            "ios_in_progress": int(parts[11]),
            "ms_io": int(parts[12]),
            "weighted_ms_io": int(parts[13]) if len(parts) > 13 else 0,
        }
    return result


def _parse_net_dev(content: str) -> dict[str, dict[str, int]]:
    """Parse /proc/net/dev. Returns dict interface->counters."""
    result = {}
    lines = content.strip().split("\n")
    for line in lines[2:]:  # skip header lines
        if ":" not in line:
            continue
        iface, rest = line.split(":", 1)
        iface = iface.strip()
        parts = rest.split()
        if len(parts) < 16:
            continue
        result[iface] = {
            "rx_bytes": int(parts[0]),
            "rx_packets": int(parts[1]),
            "rx_errs": int(parts[2]),
            "rx_drop": int(parts[3]),
            "rx_fifo": int(parts[4]),
            "rx_frame": int(parts[5]),
            "rx_compressed": int(parts[6]),
            "rx_multicast": int(parts[7]),
            "tx_bytes": int(parts[8]),
            "tx_packets": int(parts[9]),
            "tx_errs": int(parts[10]),
            "tx_drop": int(parts[11]),
            "tx_fifo": int(parts[12]),
            "tx_colls": int(parts[13]),
            "tx_carrier": int(parts[14]),
            "tx_compressed": int(parts[15]),
        }
    return result


def _parse_net_snmp(content: str) -> int | None:
    """Parse /proc/net/snmp for TCP RetransSegs."""
    lines = content.strip().split("\n")
    tcp_line = None
    for line in lines:
        if line.startswith("Tcp:"):
            if tcp_line is None:
                tcp_line = line
            else:
                # Second Tcp: line has values
                parts = line.split()
                if len(parts) > 12:
                    return int(parts[12])  # RetransSegs
    return None


def _count_tcp_connections(content: str) -> int:
    """Count TCP connections from /proc/net/tcp (each line after header is a connection)."""
    lines = content.strip().split("\n")
    return max(0, len(lines) - 1)  # subtract header


def _get_cgroup_v2_paths() -> tuple[str | None, str | None]:
    """Find cgroup v2 memory.current and memory.max paths."""
    # Try /sys/fs/cgroup first
    base = Path("/sys/fs/cgroup")
    if base.exists():
        current = base / "memory.current"
        max_path = base / "memory.max"
        if current.exists():
            return str(current), str(max_path) if max_path.exists() else None
    # Try per-process cgroup
    cgroup_content = _read_file("/proc/self/cgroup")
    if cgroup_content:
        for line in cgroup_content.strip().split("\n"):
            parts = line.split(":")
            if len(parts) == 3 and parts[1] == "":
                # unified hierarchy (v2)
                path = parts[2].strip()
                if path:
                    base_path = Path("/sys/fs/cgroup") / path.lstrip("/")
                    current = base_path / "memory.current"
                    max_path = base_path / "memory.max"
                    if current.exists():
                        return str(current), str(max_path) if max_path.exists() else None
    return None, None


class _DiffTracker:
    """Track previous samples for differential metrics."""

    def __init__(self):
        self._cpu_prev: dict[str, int] | None = None
        self._disk_prev: dict[str, dict[str, int]] | None = None
        self._net_prev: dict[str, dict[str, int]] | None = None
        self._snmp_retrans_prev: int | None = None
        self._time_prev: float | None = None

    def cpu_diff(self, current: dict[str, int]) -> dict[str, float]:
        if self._cpu_prev is None or self._time_prev is None:
            self._cpu_prev = current
            self._time_prev = time.time()
            return {"user": 0.0, "system": 0.0, "iowait": 0.0, "steal": 0.0}

        dt = time.time() - self._time_prev
        if dt <= 0:
            dt = 1.0

        total_prev = sum(self._cpu_prev.values())
        total_curr = sum(current.values())
        total_diff = total_curr - total_prev
        if total_diff <= 0:
            self._cpu_prev = current
            self._time_prev = time.time()
            return {"user": 0.0, "system": 0.0, "iowait": 0.0, "steal": 0.0}

        def pct(key: str) -> float:
            diff = current.get(key, 0) - self._cpu_prev.get(key, 0)
            return (diff / total_diff) * 100.0

        result = {
            "user": pct("user"),
            "system": pct("system"),
            "iowait": pct("iowait"),
            "steal": pct("steal"),
        }
        self._cpu_prev = current
        self._time_prev = time.time()
        return result

    def disk_diff(self, current: dict[str, dict[str, int]]) -> dict[str, dict[str, float]]:
        if self._disk_prev is None or self._time_prev is None:
            self._disk_prev = current
            return {}

        dt = time.time() - self._time_prev
        if dt <= 0:
            dt = 1.0

        result = {}
        all_devices = set(current.keys()) | set(self._disk_prev.keys())
        for dev in all_devices:
            curr = current.get(dev, {})
            prev = self._disk_prev.get(dev, {})
            if not curr or not prev:
                continue
            reads = curr.get("reads", 0) - prev.get("reads", 0)
            writes = curr.get("writes", 0) - prev.get("writes", 0)
            sectors_read = curr.get("sectors_read", 0) - prev.get("sectors_read", 0)
            sectors_written = curr.get("sectors_written", 0) - prev.get("sectors_written", 0)
            ms_read = curr.get("ms_read", 0) - prev.get("ms_read", 0)
            ms_write = curr.get("ms_write", 0) - prev.get("ms_write", 0)
            ios = curr.get("ios_in_progress", 0)
            ms_io = curr.get("ms_io", 0) - prev.get("ms_io", 0)

            # bytes per second (sector = 512 bytes)
            read_bps = (sectors_read * 512) / dt
            write_bps = (sectors_written * 512) / dt
            iops = (reads + writes) / dt
            # utilization: ms_io / (dt * 1000) * 100
            util = min(100.0, (ms_io / (dt * 1000.0)) * 100.0) if ms_io > 0 else 0.0
            # await: average ms per I/O
            total_ios = reads + writes
            await_ms = (ms_read + ms_write) / total_ios if total_ios > 0 else 0.0
            # queue: average queue length (weighted_ms_io / 1000 / dt)
            queue = (curr.get("weighted_ms_io", 0) - prev.get("weighted_ms_io", 0)) / 1000.0 / dt

            result[dev] = {
                "read_bps": read_bps,
                "write_bps": write_bps,
                "iops": iops,
                "util": util,
                "await": await_ms,
                "queue": queue,
            }
        self._disk_prev = current
        return result

    def net_diff(self, current: dict[str, dict[str, int]]) -> dict[str, dict[str, float]]:
        if self._net_prev is None or self._time_prev is None:
            self._net_prev = current
            return {}

        dt = time.time() - self._time_prev
        if dt <= 0:
            dt = 1.0

        result = {}
        all_ifaces = set(current.keys()) | set(self._net_prev.keys())
        for iface in all_ifaces:
            curr = current.get(iface, {})
            prev = self._net_prev.get(iface, {})
            if not curr or not prev:
                continue
            rx = (curr.get("rx_bytes", 0) - prev.get("rx_bytes", 0)) / dt
            tx = (curr.get("tx_bytes", 0) - prev.get("tx_bytes", 0)) / dt
            retransmit = 0.0  # from snmp separately
            drop = (curr.get("rx_drop", 0) - prev.get("rx_drop", 0) +
                    curr.get("tx_drop", 0) - prev.get("tx_drop", 0)) / dt
            error = (curr.get("rx_errs", 0) - prev.get("rx_errs", 0) +
                     curr.get("tx_errs", 0) - prev.get("tx_errs", 0)) / dt
            result[iface] = {
                "rx": rx,
                "tx": tx,
                "retransmit": retransmit,
                "drop": drop,
                "error": error,
            }
        self._net_prev = current
        return result

    def snmp_retrans_diff(self, current: int | None) -> float:
        if self._snmp_retrans_prev is None or current is None:
            self._snmp_retrans_prev = current
            return 0.0
        dt = time.time() - (self._time_prev or time.time())
        if dt <= 0:
            dt = 1.0
        diff = (current - self._snmp_retrans_prev) / dt
        self._snmp_retrans_prev = current
        return diff


_diff_tracker = _DiffTracker()


def sample_once() -> dict[str, Any]:
    """Single resource snapshot from procfs/sysfs."""
    ts = time.time()
    unavailable_reasons: dict[str, str] = {}

    # CPU
    cpu_pct = {"user": 0.0, "system": 0.0, "iowait": 0.0, "steal": 0.0}
    stat_content = _read_file("/proc/stat")
    if stat_content:
        parsed = _parse_proc_stat(stat_content)
        if parsed:
            cpu_pct = _diff_tracker.cpu_diff(parsed)
        else:
            unavailable_reasons["cpu"] = "failed to parse /proc/stat"
    else:
        unavailable_reasons["cpu"] = "/proc/stat not readable"

    load_content = _read_file("/proc/loadavg")
    load = {"load1": 0.0, "load5": 0.0, "load15": 0.0, "runqueue": 0}
    if load_content:
        parsed = _parse_loadavg(load_content)
        if parsed:
            load = parsed
        else:
            unavailable_reasons["loadavg"] = "failed to parse /proc/loadavg"
    else:
        unavailable_reasons["loadavg"] = "/proc/loadavg not readable"

    # Memory
    mem = {"rss": 0, "available": 0, "cgroup_usage": NOT_AVAILABLE, "cgroup_limit": NOT_AVAILABLE}
    status_content = _read_file("/proc/self/status")
    if status_content:
        parsed = _parse_self_status(status_content)
        mem["rss"] = parsed.get("VmRSS", 0)
    else:
        unavailable_reasons["rss"] = "/proc/self/status not readable"

    meminfo_content = _read_file("/proc/meminfo")
    if meminfo_content:
        parsed = _parse_meminfo(meminfo_content)
        mem["available"] = parsed.get("MemAvailable", 0)
    else:
        unavailable_reasons["available"] = "/proc/meminfo not readable"

    cgroup_current, cgroup_max = _get_cgroup_v2_paths()
    if cgroup_current:
        current_val = _read_file(cgroup_current)
        if current_val:
            try:
                mem["cgroup_usage"] = int(current_val.strip())
            except ValueError:
                unavailable_reasons["cgroup_usage"] = "invalid memory.current value"
        else:
            unavailable_reasons["cgroup_usage"] = "memory.current not readable"
    else:
        unavailable_reasons["cgroup_usage"] = "cgroup v2 memory.current not present"

    if cgroup_max:
        max_val = _read_file(cgroup_max)
        if max_val:
            val = max_val.strip()
            if val != "max":
                try:
                    mem["cgroup_limit"] = int(val)
                except ValueError:
                    unavailable_reasons["cgroup_limit"] = "invalid memory.max value"
            else:
                mem["cgroup_limit"] = NOT_AVAILABLE
                unavailable_reasons["cgroup_limit"] = "memory.max is 'max' (unlimited)"
        else:
            unavailable_reasons["cgroup_limit"] = "memory.max not readable"
    else:
        unavailable_reasons["cgroup_limit"] = "cgroup v2 memory.max not present"

    # Disk
    disk = {"used": 0, "free": 0, "inodes": 0,
            "read_bps": 0.0, "write_bps": 0.0, "iops": 0.0,
            "util": 0.0, "await": 0.0, "queue": 0.0, "errors": NOT_AVAILABLE}
    try:
        statvfs = os.statvfs("/")
        disk["used"] = (statvfs.f_blocks - statvfs.f_bfree) * statvfs.f_frsize
        disk["free"] = statvfs.f_bfree * statvfs.f_frsize
        disk["inodes"] = statvfs.f_files - statvfs.f_ffree
    except OSError:
        unavailable_reasons["disk_space"] = "os.statvfs failed"

    diskstats_content = _read_file("/proc/diskstats")
    if diskstats_content:
        parsed = _parse_diskstats(diskstats_content)
        diffs = _diff_tracker.disk_diff(parsed)
        # Aggregate across all devices (sum)
        total_read_bps = sum(d.get("read_bps", 0) for d in diffs.values())
        total_write_bps = sum(d.get("write_bps", 0) for d in diffs.values())
        total_iops = sum(d.get("iops", 0) for d in diffs.values())
        # util/await/queue: max across devices
        max_util = max((d.get("util", 0) for d in diffs.values()), default=0.0)
        max_await = max((d.get("await", 0) for d in diffs.values()), default=0.0)
        max_queue = max((d.get("queue", 0) for d in diffs.values()), default=0.0)

        disk["read_bps"] = total_read_bps
        disk["write_bps"] = total_write_bps
        disk["iops"] = total_iops
        disk["util"] = max_util
        disk["await"] = max_await
        disk["queue"] = max_queue
    else:
        unavailable_reasons["disk_io"] = "/proc/diskstats not readable"

    # Network
    net = {"rx": 0.0, "tx": 0.0, "retransmit": 0.0, "drop": 0.0, "error": 0.0, "conns": 0}
    netdev_content = _read_file("/proc/net/dev")
    if netdev_content:
        parsed = _parse_net_dev(netdev_content)
        diffs = _diff_tracker.net_diff(parsed)
        # Aggregate across all interfaces (sum)
        total_rx = sum(d.get("rx", 0) for d in diffs.values())
        total_tx = sum(d.get("tx", 0) for d in diffs.values())
        total_drop = sum(d.get("drop", 0) for d in diffs.values())
        total_error = sum(d.get("error", 0) for d in diffs.values())
        net["rx"] = total_rx
        net["tx"] = total_tx
        net["drop"] = total_drop
        net["error"] = total_error
    else:
        unavailable_reasons["net_io"] = "/proc/net/dev not readable"

    snmp_content = _read_file("/proc/net/snmp")
    if snmp_content:
        retrans = _parse_net_snmp(snmp_content)
        net["retransmit"] = _diff_tracker.snmp_retrans_diff(retrans)
    else:
        unavailable_reasons["retransmit"] = "/proc/net/snmp not readable"

    tcp_content = _read_file("/proc/net/tcp")
    if tcp_content:
        net["conns"] = _count_tcp_connections(tcp_content)
    else:
        unavailable_reasons["conns"] = "/proc/net/tcp not readable"

    return {
        "ts": ts,
        "cpu": {
            "user": cpu_pct.get("user", 0.0),
            "system": cpu_pct.get("system", 0.0),
            "iowait": cpu_pct.get("iowait", 0.0),
            "steal": cpu_pct.get("steal", 0.0),
            "load1": load.get("load1", 0.0),
            "load5": load.get("load5", 0.0),
            "load15": load.get("load15", 0.0),
            "runqueue": load.get("runqueue", 0),
        },
        "mem": mem,
        "disk": disk,
        "net": net,
        "unavailable_reasons": unavailable_reasons,
    }


@dataclass
class ResourceSampler:
    """Background resource sampler writing ndjson."""
    interval: float = 1.0
    out_path: Path = field(default_factory=lambda: Path("/tmp/resources.ndjson"))

    _thread: threading.Thread | None = field(default=None, init=False)
    _stop_event: threading.Event = field(default_factory=threading.Event, init=False)
    _file_handle: Any = field(default=None, init=False)

    def start(self) -> None:
        if self._thread is not None and self._thread.is_alive():
            return
        self._stop_event.clear()
        self._file_handle = open(self.out_path, "a", buffering=1)  # line buffered
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def _run(self) -> None:
        while not self._stop_event.is_set():
            sample = sample_once()
            self._file_handle.write(json.dumps(sample) + "\n")
            self._file_handle.flush()
            self._stop_event.wait(self.interval)

    def stop(self) -> None:
        if self._thread is None:
            return
        self._stop_event.set()
        self._thread.join(timeout=5.0)
        if self._file_handle:
            self._file_handle.flush()
            self._file_handle.close()
            self._file_handle = None
        self._thread = None


def aggregate_windows(rows: list[dict], window_s: int = 10) -> list[dict]:
    """Aggregate samples into fixed 10-second windows."""
    if not rows:
        return []

    # Sort by timestamp
    rows = sorted(rows, key=lambda r: r.get("ts", 0))

    # Determine window boundaries from first sample
    first_ts = rows[0].get("ts", 0)
    window_start = int(first_ts // window_s) * window_s

    windows: dict[int, list[dict]] = {}
    for row in rows:
        ts = row.get("ts", 0)
        win_idx = int((ts - window_start) // window_s)
        if win_idx < 0:
            win_idx = 0
        win_key = window_start + win_idx * window_s
        windows.setdefault(win_key, []).append(row)

    result = []
    for win_start in sorted(windows.keys()):
        win_rows = windows[win_start]
        win_end = win_start + window_s

        def agg(key_path: list[str], op: str = "avg") -> float | str:
            """Aggregate a nested key across rows. Skip NOT_AVAILABLE."""
            vals = []
            for r in win_rows:
                cur = r
                for k in key_path:
                    if isinstance(cur, dict):
                        cur = cur.get(k, NOT_AVAILABLE)
                    else:
                        cur = NOT_AVAILABLE
                        break
                if cur != NOT_AVAILABLE and isinstance(cur, (int, float)):
                    vals.append(float(cur))
            if not vals:
                return NOT_AVAILABLE
            if op == "avg":
                return sum(vals) / len(vals)
            elif op == "max":
                return max(vals)
            return NOT_AVAILABLE

        cpu_avg = {k: agg(["cpu", k], "avg") for k in ("user", "system", "iowait", "steal", "load1", "load5", "load15", "runqueue")}
        cpu_max = {k: agg(["cpu", k], "max") for k in ("user", "system", "iowait", "steal", "load1", "load5", "load15", "runqueue")}
        mem_avg = {k: agg(["mem", k], "avg") for k in ("rss", "available", "cgroup_usage", "cgroup_limit")}
        mem_max = {k: agg(["mem", k], "max") for k in ("rss", "available", "cgroup_usage", "cgroup_limit")}
        disk_avg = {k: agg(["disk", k], "avg") for k in ("used", "free", "inodes", "read_bps", "write_bps", "iops", "util", "await", "queue")}
        disk_max = {k: agg(["disk", k], "max") for k in ("used", "free", "inodes", "read_bps", "write_bps", "iops", "util", "await", "queue")}
        net_avg = {k: agg(["net", k], "avg") for k in ("rx", "tx", "retransmit", "drop", "error", "conns")}
        net_max = {k: agg(["net", k], "max") for k in ("rx", "tx", "retransmit", "drop", "error", "conns")}

        result.append({
            "window_start": win_start,
            "window_end": win_end,
            "samples": len(win_rows),
            "cpu": {"avg": cpu_avg, "max": cpu_max},
            "mem": {"avg": mem_avg, "max": mem_max},
            "disk": {"avg": disk_avg, "max": disk_max},
            "net": {"avg": net_avg, "max": net_max},
        })
    return result


if __name__ == "__main__":
    # Quick smoke test
    print("Sample once:")
    import json as _json
    print(_json.dumps(sample_once(), indent=2)[:800])

    print("\nTesting sampler...")
    import tempfile
    with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".ndjson") as f:
        tmp = f.name
    sampler = ResourceSampler(interval=0.1, out_path=Path(tmp))
    sampler.start()
    time.sleep(0.35)
    sampler.stop()
    with open(tmp) as f:
        lines = f.readlines()
    print(f"Lines written: {len(lines)}")
    os.unlink(tmp)

    print("\nTesting aggregate_windows...")
    # Synthetic rows
    base = 1000.0
    synthetic = []
    for i in range(25):
        synthetic.append({
            "ts": base + i * 0.5,
            "cpu": {"user": 10.0 + i, "system": 5.0, "iowait": 1.0, "steal": 0.0, "load1": 1.0, "load5": 1.0, "load15": 1.0, "runqueue": 2},
            "mem": {"rss": 100000000, "available": 5000000000, "cgroup_usage": NOT_AVAILABLE, "cgroup_limit": NOT_AVAILABLE},
            "disk": {"used": 10000000000, "free": 50000000000, "inodes": 100000, "read_bps": 1000000, "write_bps": 500000, "iops": 100, "util": 10.0, "await": 2.0, "queue": 0.5},
            "net": {"rx": 100000, "tx": 50000, "retransmit": 0, "drop": 0, "error": 0, "conns": 10},
            "unavailable_reasons": {}
        })
    windows = aggregate_windows(synthetic)
    print(f"Windows: {len(windows)}")
    for w in windows:
        print(f"  {w['window_start']}-{w['window_end']}: samples={w['samples']}, cpu_user_avg={w['cpu']['avg']['user']}, cpu_user_max={w['cpu']['max']['user']}")