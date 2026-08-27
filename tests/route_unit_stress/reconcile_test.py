#!/usr/bin/env python3
"""Synthetic data self-tests for lib_reconcile (S11).

Covers: PASS, DATA_INVALID (missing, duplicate, mismatch), attempt gaps.
Run: python3 tests/route_unit_stress/reconcile_test.py
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib_reconcile import reconcile, ReconcileResult


def make_audit_attempt(
    client_request_id: str,
    attempt: int,
    group: str = "default",
    alias: str = "test-alias",
    channel_id: int = 90001,
    key_index: int = 0,
    upstream_model: str = "mock-ok",
    outcome: int = 0,
) -> dict:
    return {
        "request_id": f"req-{client_request_id}",
        "client_request_id": client_request_id,
        "attempt": attempt,
        "group": group,
        "alias": alias,
        "channel_id": channel_id,
        "key_index": key_index,
        "upstream_model": upstream_model,
        "outcome": outcome,
    }


def make_mock_row(
    request_id: str,
    upstream_model: str = "mock-ok",
    status: int = 200,
    mode: str = "ok",
) -> dict:
    return {
        "ts": 1234567890.0,
        "request_id": request_id,
        "upstream_model": upstream_model,
        "status": status,
        "mode": mode,
    }


def test_pass() -> bool:
    """A retry chain matches when every attempt has its own upstream call.

    Two audit attempts means the upstream was called twice, so two mock rows are
    the correct shape. The second attempt sits on a different channel because
    controller/relay.go excludes the failed route unit from the retry.
    """
    crid = "test-pass-1"
    attempts = [
        make_audit_attempt(crid, 0, channel_id=90001),
        make_audit_attempt(crid, 1, channel_id=90002),
    ]
    mock = [make_mock_row(crid), make_mock_row(crid)]
    result = reconcile(attempts, mock)
    assert result.verdict == "PASS", f"Expected PASS, got {result.verdict}: {result.to_summary()}"
    assert result.matched_pairs == 1
    assert result.total_requests == 1
    assert result.attempt_count_mismatch == []
    print("✓ test_pass")
    return True


def test_missing_in_audit() -> bool:
    """Request exists in mock but not in audit."""
    attempts = [make_audit_attempt("crid-1", 0)]
    mock = [
        make_mock_row("crid-1"),
        make_mock_row("crid-missing"),
    ]
    result = reconcile(attempts, mock)
    assert result.verdict == "DATA_INVALID"
    assert "crid-missing" in result.missing_in_audit
    print("✓ test_missing_in_audit")
    return True


def test_missing_in_mock() -> bool:
    """Request exists in audit but not in mock."""
    attempts = [
        make_audit_attempt("crid-1", 0),
        make_audit_attempt("crid-missing", 0),
    ]
    mock = [make_mock_row("crid-1")]
    result = reconcile(attempts, mock)
    assert result.verdict == "DATA_INVALID"
    assert "crid-missing" in result.missing_in_mock
    print("✓ test_missing_in_mock")
    return True


def test_extra_mock_row_without_attempt() -> bool:
    """More upstream calls than recorded attempts is a real inconsistency.

    One audited attempt but two upstream calls means a call was made that the
    gateway never charged to any route, which is exactly the audit blind spot
    #418 is checking for. This replaces the old "duplicate mock row" rule, which
    treated a legitimate retry chain as corrupt.
    """
    attempts = [make_audit_attempt("crid-1", 0)]
    mock = [
        make_mock_row("crid-1"),
        make_mock_row("crid-1"),
    ]
    result = reconcile(attempts, mock)
    assert result.verdict == "DATA_INVALID"
    assert any(
        m["request_id"] == "crid-1" and m["audit_attempts"] == 1 and m["mock_rows"] == 2
        for m in result.attempt_count_mismatch
    ), result.attempt_count_mismatch
    print("✓ test_extra_mock_row_without_attempt")
    return True


def test_upstream_model_mismatch() -> bool:
    """upstream_model differs between audit and mock."""
    attempts = [make_audit_attempt("crid-1", 0, upstream_model="mock-fast")]
    mock = [make_mock_row("crid-1", upstream_model="mock-slow")]
    result = reconcile(attempts, mock)
    assert result.verdict == "DATA_INVALID"
    assert any(m.get("type") == "upstream_model_mismatch" for m in result.identity_mismatch)
    print("✓ test_upstream_model_mismatch")
    return True


def test_incomplete_audit_identity() -> bool:
    """An attempt missing part of its quadruple cannot be attributed to a route.

    A retry landing on a DIFFERENT channel is correct behaviour (the failed route
    is excluded), so differing identities across a chain is not an error. What is
    an error is an attempt whose own identity is incomplete.
    """
    attempts = [make_audit_attempt("crid-1", 0, upstream_model="")]
    mock = [make_mock_row("crid-1", upstream_model="")]
    result = reconcile(attempts, mock)
    assert result.verdict == "DATA_INVALID"
    assert any(m.get("type") == "incomplete_audit_identity" for m in result.identity_mismatch), \
        result.identity_mismatch
    print("✓ test_incomplete_audit_identity")
    return True


def test_retry_across_channels_is_not_a_mismatch() -> bool:
    """A retry chain spanning two channels reconciles cleanly.

    Regression guard: the earlier rule required every attempt of a request to
    share one identity tuple, which flagged 24 of 600 correct retry chains as
    DATA_INVALID on a live gateway.
    """
    attempts = [
        make_audit_attempt("crid-1", 0, channel_id=90001, outcome=2),
        make_audit_attempt("crid-1", 1, channel_id=90002, outcome=0),
    ]
    mock = [make_mock_row("crid-1", status=503), make_mock_row("crid-1")]
    result = reconcile(attempts, mock)
    assert result.verdict == "PASS", result.to_summary()
    assert result.identity_mismatch == []
    print("✓ test_retry_across_channels_is_not_a_mismatch")
    return True


def test_attempt_gap() -> bool:
    """Attempt numbers not continuous (e.g., 0, 2 missing 1)."""
    attempts = [
        make_audit_attempt("crid-1", 0),
        make_audit_attempt("crid-1", 2),  # gap: missing attempt 1
    ]
    mock = [make_mock_row("crid-1")]
    result = reconcile(attempts, mock)
    assert result.verdict == "DATA_INVALID"
    assert len(result.attempt_gaps) == 1
    assert result.attempt_gaps[0]["request_id"] == "crid-1"
    assert result.attempt_gaps[0]["actual"] == [0, 2]
    assert result.attempt_gaps[0]["expected"] == [0, 1]
    print("✓ test_attempt_gap")
    return True


def test_multiple_requests_mixed() -> bool:
    """Multiple requests with mix of pass and fail."""
    attempts = [
        make_audit_attempt("crid-pass", 0),
        make_audit_attempt("crid-pass", 1),
        make_audit_attempt("crid-gap", 0),
        make_audit_attempt("crid-gap", 2),  # gap
        make_audit_attempt("crid-missing-mock", 0),
    ]
    mock = [
        make_mock_row("crid-pass"),
        make_mock_row("crid-gap"),
        # crid-missing-mock not in mock
    ]
    result = reconcile(attempts, mock)
    assert result.verdict == "DATA_INVALID"
    assert result.total_requests == 3
    assert result.matched_pairs == 2
    assert "crid-missing-mock" in result.missing_in_mock
    assert len(result.attempt_gaps) == 1
    assert result.attempt_gaps[0]["request_id"] == "crid-gap"
    print("✓ test_multiple_requests_mixed")
    return True


def test_empty_inputs() -> bool:
    """Both inputs empty."""
    result = reconcile([], [])
    assert result.verdict == "PASS"
    assert result.total_requests == 0
    assert result.matched_pairs == 0
    print("✓ test_empty_inputs")
    return True


def test_audit_without_client_request_id() -> bool:
    """Audit rows missing client_request_id are ignored."""
    attempts = [
        {"request_id": "req-1", "attempt": 0, "group": "g", "alias": "a", "channel_id": 1, "key_index": 0, "upstream_model": "m", "outcome": 0},
        make_audit_attempt("crid-1", 0),
    ]
    mock = [make_mock_row("crid-1")]
    result = reconcile(attempts, mock)
    # First attempt has no client_request_id -> ignored
    # Only crid-1 is counted
    assert result.verdict == "PASS"
    assert result.total_requests == 1
    assert result.matched_pairs == 1
    print("✓ test_audit_without_client_request_id")
    return True


def test_zero_samples() -> bool:
    """Zero matched pairs with expected_requests > 0 -> DATA_INVALID."""
    attempts = []
    mock = []
    result = reconcile(attempts, mock, expected_requests=10)
    assert result.verdict == "DATA_INVALID", f"Expected DATA_INVALID, got {result.verdict}"
    assert result.matched_pairs == 0
    assert result.total_requests == 0
    print("✓ test_zero_samples")
    return True


def test_buffer_residue_filtered_pass() -> bool:
    """Audit buffer contains previous run residue; filtering by expected IDs yields PASS."""
    # Previous run: 5 requests (crid-old-0..4)
    old_attempts = [make_audit_attempt(f"crid-old-{i}", 0) for i in range(5)]
    old_mock = [make_mock_row(f"crid-old-{i}") for i in range(5)]

    # Current run: 3 requests (crid-new-0..2)
    new_attempts = [make_audit_attempt(f"crid-new-{i}", 0) for i in range(3)]
    new_mock = [make_mock_row(f"crid-new-{i}") for i in range(3)]

    # Combined buffer (simulating ring buffer accumulation)
    all_attempts = old_attempts + new_attempts
    all_mock = old_mock + new_mock

    # Without filtering: PASS but counts include residue (8 matched, not 3)
    result_unfiltered = reconcile(all_attempts, all_mock, expected_requests=3)
    assert result_unfiltered.verdict == "PASS"
    assert result_unfiltered.matched_pairs == 8  # residue inflates count

    # With filtering by current run's request IDs: PASS, scoped to 3
    expected_ids = {f"crid-new-{i}" for i in range(3)}
    result_filtered = reconcile(
        all_attempts, all_mock,
        expected_requests=3,
        expected_request_ids=expected_ids,
    )
    assert result_filtered.verdict == "PASS", f"Expected PASS, got {result_filtered.verdict}"
    assert result_filtered.matched_pairs == 3
    assert result_filtered.total_records == 8  # pre-filter total
    assert result_filtered.scoped_records == 3  # post-filter total
    assert result_filtered.total_requests == 3
    print("✓ test_buffer_residue_filtered_pass")
    return True


def run_all() -> int:
    tests = [
        test_pass,
        test_missing_in_audit,
        test_missing_in_mock,
        test_extra_mock_row_without_attempt,
        test_upstream_model_mismatch,
        test_incomplete_audit_identity,
        test_retry_across_channels_is_not_a_mismatch,
        test_attempt_gap,
        test_multiple_requests_mixed,
        test_empty_inputs,
        test_audit_without_client_request_id,
        test_zero_samples,
        test_buffer_residue_filtered_pass,
    ]
    passed = 0
    failed = 0
    for t in tests:
        try:
            t()
            passed += 1
        except AssertionError as e:
            print(f"✗ {t.__name__}: {e}")
            failed += 1
        except Exception as e:
            print(f"✗ {t.__name__}: unexpected error: {e}")
            failed += 1
    print(f"\n=== {passed} passed, {failed} failed ===")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(run_all())