# Task: context-range model dispatch scheduling

## Background

Today every public model alias maps to one flat weighted pool of
`channel_model_routes`. `SelectRouteUnit` treats all enabled units of an
alias as one population and runs P2C within it. There is no notion of
"this alias's scheduling must change based on input context size", and no way
for one alias to delegate/fall back to another alias's scheduling.

This task turns model dispatch into a multi-strategy, context-aware
scheduler while keeping the existing weighted-pool behaviour as the default.

## Requested behaviour

1. Model dispatch becomes multi-select/multi-strategy:
   - a model alias can declare how it schedules (weighted pool, context range, or
     delegation to another alias);
   - one alias may reference another alias's scheduling (fallback/delegate).
2. Add a new scheduling mode **context split**:
   - an alias knows its context-size correspondence (request input token count);
   - a context route declares a `[context_min, context_max]` interval;
   - a fixed set of channel models is used inside that interval;
   - a default/fallback route is used when no interval matches or its channels
     are unavailable.
3. Routing health UI is updated to manage and visualise these strategies.

## Phases

### Phase 1 — plumbing + data model (backend only, no behaviour change)

- Add `InputTokens int` to `SelectParams` and `SelectRouteUnit` signature (thread
  through all callers: distribute middleware, selected route helpers, tests).
- New tables:
  - `model_alias_dispatch_policy(alias PK, mode, fallback_alias, updated_at)`
  - `model_alias_context_routes(id, alias, context_min, context_max,
    channel_id, key_index, upstream_model, static_weight, enabled, sort_order)`
- Migration + seed: every existing alias gets `mode=weighted_pool` so behaviour
  is identical.
- Keep `channel_model_routes` and EWMA stats identity unchanged.

### Phase 2 — selection logic + validation + cache (backend)

- `SelectRouteUnit`: read policy for alias:
  - `weighted_pool`: current behaviour.
  - `context_range`: pick the matching interval by `InputTokens`; fallback to the
    default `[0, ∞)` rule or the configured fallback alias when none matches or
    all its channels are down.
  - `delegate`: resolve the fallback alias (cycle-safe) and reuse its policy.
- Validation endpoints for interval overlap, bounds, fallback existence.
- `InitChannelCache` / cache reload must include the new tables.
- Update `handle_route_unit.go` API and OpenAPI snapshot.

### Phase 3 — management UI (frontend)

- In `route-units-section.tsx`: alias-level strategy selector
  (weighted pool / context range / delegate-to-alias).
- Multi-select channel dialog per context interval, reusing the existing
  checkbox selector.
- Context interval editor with `10k` / `1.5m` shorthand and fallback selector.
- Route Units alias list shows strategy badges.

### Phase 4 — routing health UI + i18n + verification

- Health table grouped by context interval.
- Playwright screenshots for review before merge.
- i18n keys for all supported locales.

## Files likely to change

Backend:
- `apps/api/internal/catalog/store_model_routes.go`
- `apps/api/internal/catalog/select_route_unit.go`
- `apps/api/internal/catalog/select_channel_cached.go`
- `apps/api/internal/catalog/selector_seam.go`
- `apps/api/internal/transport/handler/handle_route_unit.go`
- `apps/api/internal/transport/middleware/distribute_channel.go`
- bootstrap/migrations + tests

Frontend:
- `apps/web/src/features/system-settings/models/route-units-section.tsx`
- `apps/web/src/features/system-settings/models/api-route-units.ts`
- new context-range editor component
- `apps/web/src/i18n/locales/*.json`

## Acceptance criteria

- Existing aliases behave exactly as before (weighted pool default).
- A context-range alias routes requests to the interval matching input token
  count; fallback works when the interval is unavailable.
- All backend changes compile in CI; no local heavy builds.
- Routing health UI screenshot verified by maintainer before merge.

## Open questions

1. Delegation is modelled as "fallback alias chain" in this plan; confirm if
   stronger "inherit entire policy" is required.
2. Input token source is request-token estimate; confirm whether model-declared
   context window should also be supported as a selector.
3. Confirm whether context intervals should be exclusive (recommended) or
   overlapping with priority order.

## Testing policy

- Backend/unit changes are validated in GitHub CI, not locally.
- Final UI changes require Playwright screenshots for manual review before
  merge; do not auto-merge.
