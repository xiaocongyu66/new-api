#!/usr/bin/env python3
"""Core reconciliation logic for route-unit EWMA stress data chain (S11).

Joins gateway audit attempts with mock upstream ndjson on client_request_id/request_id.
Validates identity tuple consistency (audit-side) and upstream_model match (cross-side).
Checks attempt sequence continuity.

Verdict rules:
- PASS: all expected requests matched, no missing/duplicate/mismatch/gaps
- DATA_INVALID: expected_requests > 0 and (matched < expected_requests OR any missing/duplicate/mismatch/gaps)
- ENV_UNREACHABLE: not used by this module (handled by caller)

Filtering: if expected_request_ids is provided, both attempts and upstream_rows are
filtered to only those request_ids before reconciliation. This handles audit ring
buffer accumulation across runs.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class ReconcileResult:
    """Result of a reconciliation run."""
    verdict: str  # PASS | DATA_INVALID | ENV_UNREACHABLE
    total_requests: int = 0
    matched_pairs: int = 0
    missing_in_audit: list[str] = field(default_factory=list)      # request_ids in mock but not audit
    missing_in_mock: list[str] = field(default_factory=list)       # request_ids in audit but not mock
    duplicate_in_audit: list[str] = field(default_factory=list)    # request_ids with duplicate audit rows
    duplicate_in_mock: list[str] = field(default_factory=list)     # request_ids with duplicate mock rows
    identity_mismatch: list[dict[str, Any]] = field(default_factory=list)  # tuple diffs (audit internal + cross-side model)
    attempt_gaps: list[dict[str, Any]] = field(default_factory=list)       # non-continuous attempt sequences
    # New fields for pre/post filter counts
    total_records: int = 0         # total unique request_ids seen before filtering
    scoped_records: int = 0        # total unique request_ids after filtering (== expected_request_ids count)

    def to_summary(self) -> dict[str, Any]:
        return {
            "verdict": self.verdict,
            "total_requests": self.total_requests,
            "matched_pairs": self.matched_pairs,
            "missing_in_audit": self.missing_in_audit,
            "missing_in_mock": self.missing_in_mock,
            "duplicate_in_audit": self.duplicate_in_audit,
            "duplicate_in_mock": self.duplicate_in_mock,
            "identity_mismatch": self.identity_mismatch,
            "attempt_gaps": self.attempt_gaps,
            "total_records": self.total_records,
            "scoped_records": self.scoped_records,
        }


AUDIT_IDENTITY_FIELDS = ("group", "alias", "channel_id", "key_index", "upstream_model")


def _audit_identity_tuple(row: dict[str, Any]) -> tuple:
    """Extract the 5-field identity tuple from an audit attempt row."""
    return tuple(row.get(f) for f in AUDIT_IDENTITY_FIELDS)


def reconcile(
    attempts: list[dict[str, Any]],
    upstream_rows: list[dict[str, Any]],
    expected_requests: int = 0,
    expected_request_ids: set[str] | None = None,
) -> ReconcileResult:
    """Reconcile gateway audit attempts against mock upstream received rows.

    Args:
        attempts: List of audit attempt dicts from GET /api/route_unit/audit.
                  Each must have: request_id, client_request_id, attempt,
                  group, alias, channel_id, key_index, upstream_model, outcome.
        upstream_rows: List of mock ndjson dicts. Each must have: request_id,
                       upstream_model, status, mode, ts.
        expected_requests: Number of client requests sent. Used for verdict logic.
                           If > 0 and matched_pairs < expected_requests, verdict=DATA_INVALID.
        expected_request_ids: Optional set of request_ids that belong to this run.
                              If provided, both attempts and upstream_rows are filtered
                              to only these IDs before reconciliation. This handles
                              audit ring buffer accumulation across runs.

    Returns:
        ReconcileResult with verdict and detailed mismatch lists.
    """
    # Index audit attempts by client_request_id
    audit_by_crid: dict[str, list[dict[str, Any]]] = {}
    for a in attempts:
        crid = a.get("client_request_id") or a.get("clientRequestId") or ""
        if not crid:
            continue
        audit_by_crid.setdefault(crid, []).append(a)

    # Index mock rows by request_id
    mock_by_rid: dict[str, list[dict[str, Any]]] = {}
    for m in upstream_rows:
        rid = m.get("request_id") or ""
        if not rid:
            continue
        mock_by_rid.setdefault(rid, []).append(m)

    # Track pre-filter total unique request_ids
    all_crids_pre_filter = set(audit_by_crid.keys()) | set(mock_by_rid.keys())
    total_records = len(all_crids_pre_filter)

    # Filter to expected request IDs if provided
    if expected_request_ids is not None:
        audit_by_crid = {k: v for k, v in audit_by_crid.items() if k in expected_request_ids}
        mock_by_rid = {k: v for k, v in mock_by_rid.items() if k in expected_request_ids}

    all_crids = set(audit_by_crid.keys()) | set(mock_by_rid.keys())
    scoped_records = len(all_crids)
    result = ReconcileResult(verdict="PASS", total_requests=len(all_crids),
                            total_records=total_records, scoped_records=scoped_records)

    for crid in sorted(all_crids):
        audit_rows = audit_by_crid.get(crid, [])
        mock_rows = mock_by_rid.get(crid, [])

        # Missing entirely on one side
        if not audit_rows:
            result.missing_in_audit.append(crid)
            result.verdict = "DATA_INVALID"
            continue
        if not mock_rows:
            result.missing_in_mock.append(crid)
            result.verdict = "DATA_INVALID"
            continue

        # Duplicate rows on mock side (should be exactly 1 per request_id)
        if len(mock_rows) > 1:
            result.duplicate_in_mock.append(crid)
            result.verdict = "DATA_INVALID"

        # Identity check:
        # 1. All audit rows for this crid must have identical identity tuple
        # 2. upstream_model must match between audit (first row) and mock (first row)
        if audit_rows:
            first_audit_identity = _audit_identity_tuple(audit_rows[0])
            for a in audit_rows[1:]:
                if _audit_identity_tuple(a) != first_audit_identity:
                    result.identity_mismatch.append({
                        "request_id": crid,
                        "type": "audit_internal_inconsistent",
                        "expected": dict(zip(AUDIT_IDENTITY_FIELDS, first_audit_identity)),
                        "actual": dict(zip(AUDIT_IDENTITY_FIELDS, _audit_identity_tuple(a))),
                    })
                    result.verdict = "DATA_INVALID"

            # Cross-side upstream_model match
            if mock_rows:
                audit_model = audit_rows[0].get("upstream_model")
                mock_model = mock_rows[0].get("upstream_model")
                if audit_model != mock_model:
                    result.identity_mismatch.append({
                        "request_id": crid,
                        "type": "upstream_model_mismatch",
                        "audit": audit_model,
                        "mock": mock_model,
                    })
                    result.verdict = "DATA_INVALID"

        # Attempt sequence continuity: attempts must be 0,1,2... no gaps
        attempt_nums = sorted(a.get("attempt", -1) for a in audit_rows)
        if attempt_nums:
            expected = list(range(len(attempt_nums)))
            if attempt_nums != expected:
                result.attempt_gaps.append({
                    "request_id": crid,
                    "expected": expected,
                    "actual": attempt_nums,
                })
                result.verdict = "DATA_INVALID"

        # Count matched pairs (one mock row + at least one audit row)
        result.matched_pairs += 1

    # Zero-samples rule: if we expected requests but matched fewer, it's DATA_INVALID
    if expected_requests > 0 and result.matched_pairs < expected_requests:
        result.verdict = "DATA_INVALID"

    return result