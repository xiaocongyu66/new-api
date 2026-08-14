---
title: Karmada Phase 2 Core Features
date: 2026-08-14
status: approved
---

# Karmada Phase 2 Core Features — Design

## Problem

Epic #103 Phase 1 provides the authenticated Karmada client, member-cluster list/detail endpoints, proxy path, monitoring manifests, and a Dioxus shell, but the three Phase 2 tabs remain placeholders. Operators need one admin surface for cluster health, distributed resources, and policy CRUD.

## Goals

- Implement #112 Cluster Overview end to end: cluster metadata, node counts, resource utilization, synchronization P95, on-demand detail counts/nodes/events, and Dioxus UI.
- Implement #111 Resource Management end to end: supported resource listing and filtering, cross-cluster workload distribution, Deployment scaling, deletion with confirmation, audit coverage, Secret data redaction, and Dioxus UI.
- Implement #113 Policy Management end to end: four policy kinds, list filters, detail YAML, matched-resource view through ResourceBinding references, create/update/delete, basic YAML and kind validation, namespace-aware routes, and Dioxus UI.
- Keep each issue independently reviewable from `feat/karmada-phase1-integration`, with focused tests and a recorded review result.

## Non-goals

- Pod logs, exec, terminal access, resource creation, dry-run simulation, or policy-effect simulation.
- A new Karmada Go dependency; the existing authenticated HTTP proxy remains the integration boundary.
- A new frontend framework or a heavyweight WASM syntax-highlighting dependency.
- Historical metric charts or streaming updates.

## Constraints

- Backend changes stay in the existing flat `app/api/controller` package and use `common` JSON helpers.
- All Karmada routes remain admin-only and use the existing `AdminAuth + RequirePermission(authz.SystemSettings)` middleware.
- Karmada API paths are constructed from allowlisted resource kinds and escaped names/namespaces; arbitrary upstream paths are never accepted by the feature endpoints.
- Prometheus is queried server-side through an optional `PROMETHEUS_URL`; unavailable metrics are represented as null/unknown data, not fabricated zeros.
- Cluster status uses the documented `spec.apiEndpoint`, `status.nodeSummary.readyNum/totalNum`, `status.kubernetesVersion`, and `status.conditions` fields. Synchronization P95 uses the recording rule `karmada:cluster_sync:latency:p95` when the metric is available.
- Resource writes operate on Karmada control-plane resource templates. Cross-cluster distribution reads ResourceBinding `spec.clusters` and uses Work objects only for per-cluster detail/status. Secret list/detail responses never include `data` or `stringData`.
- Policy routes distinguish namespaced kinds (`PropagationPolicy`, `OverridePolicy`) from cluster-scoped kinds (`ClusterPropagationPolicy`, `ClusterOverridePolicy`). YAML submission must parse as one object and match the four-kind allowlist before forwarding.
- The existing generic admin audit middleware records all write operations; resource handlers additionally record stable action parameters where the existing controller audit helpers support it.

## Approach

Add typed controller DTOs and small helpers to the existing `karmada.go` module. Cluster endpoints enrich Karmada Cluster objects with Prometheus query results and on-demand aggregated API queries. Resource endpoints map the eight allowed Kubernetes kinds to their canonical API paths, list either through the Karmada API or aggregated member-cluster proxy, redact Secrets, and use JSON merge/scale subresource requests for Deployment replicas. Policy endpoints use a single policy-kind route resolver, forward JSON bodies to the appropriate Karmada API endpoint, and return the raw policy object plus matched ResourceBinding references. Each handler returns the established `{success,data}` envelope.

Extend the Dioxus shell in one source file with three real tab views, browser `fetch` calls, loading/error/empty states, responsive tables, detail drawers/panels, confirmation before destructive actions, and a small CSS YAML presentation using `<pre>` with stable line wrapping. The UI uses no extra crate: existing web dependencies are reused and the browser fetch API is invoked through Dioxus document evaluation only where needed.

Use one branch and one PR per issue, all based on `feat/karmada-phase1-integration`; no merge, push, or issue closure is performed by this implementation run.

## Alternatives considered

A staged implementation beginning with #112 would reduce initial uncertainty but would leave two explicit Phase 2 deliverables incomplete and force a second design/verification cycle. A new Kubernetes client dependency would provide typed APIs but duplicate the existing proxy boundary, add dependency/build weight, and complicate WASM/backend separation. The selected approach keeps the HTTP contract explicit and limits new code to the requested vertical slices.

## Testing

Backend tests use `httptest.Server` to assert exact upstream paths, query filters, request methods/bodies, authorization behavior, response mapping, Secret redaction, scaling, deletion, policy namespace routing, YAML rejection, and Prometheus fallback. Dioxus is built with the existing release web build script. Each issue branch runs focused Go tests, `git diff --check`, and the Dioxus build; the integration branch receives a final combined build and CRG/review pass. Browser smoke checks exercise the built panel's three tabs where a runnable embedded server is available.

## Open questions

None. Missing Prometheus series remain explicitly unknown; the Karmada API remains the source of truth for metadata and writes.
