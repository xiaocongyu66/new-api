# S5 (#476) Orchestrator Findings — read before touching anything

Base: `bed5e0412` on `chore/phase0-correct`. Worktree `/home/hathaway/projects/new-api/.wt/phase0-correct`.

## Verified preconditions

| Item | Value |
|---|---|
| `controller/` non-test | 39 (61 total) |
| `service/` non-test | 60 (82 total) |
| `model/` non-test | 44 (80 total) |
| Test baseline | **33 fails, 0 panics** → `/tmp/s5-base.txt` |
| `main-410` reference | `50ef5fa3e` (read-only) |

## Blast radius (measured)

| Package | Imported by | Refs | Distinct symbols |
|---|---|---|---|
| `controller` | 8 files | 198 | 128 |
| `service` | 102 files | 431 | 100 |
| `model` | 155 files | 1663 | 318 |

`model` is the dominant risk: 1663 call sites must each learn which domain their
symbol landed in.

## Probe result 1: ZERO import cycles (decisive)

I dry-ran the **full** §4 model split (33 files → catalog/usage/billing/task/ops/egress),
then asked the compiler:

```
go build ./... 2>&1 | grep -c 'import cycle'   →  0
```

Total breakage was only 12 lines / 4 undefined symbols. **The split is viable.**
Do not redesign it.

## Probe result 2: `health_fallback.go` is misrouted in the taskbook

The only file that broke was `model/health_fallback.go`. Its 4 symbols
(`localHealthManager`, `ChannelOutcome`, `ChannelHealthState`, `ChannelAttempt`)
are all defined in `model/channel_health.go`, which §4 sends to **catalog**.

§4 sends `health_fallback.go` to `internal/common/dbx/`. That is wrong.

**Correction: `model/health_fallback.go` → `internal/catalog/fallback_health.go`**
(not dbx). This drops the dbx infra batch from 11 files to 10.

## Probe result 3: dbx batch MUST be last

`model/main.go` (AutoMigrate) and `model/migrations.go` reference nearly every
record type: `Channel`, `Redemption`, `Ability`, `SubscriptionPlan`,
`ChannelModelHealth`, `InitializeGatewayConfigRevision`, `Log`, …

They only compile once **all** record types have left `model/`. So:

```
A1 → A2 → A3 → A4.1 (service→common) → A4.2 (model infra→dbx) → A4.3 (flatten)
```

`A4.2` is the batch that finally deletes `model/`. It cannot move earlier.

## Package-name decision (IMPORTANT)

`internal/catalog/` declares `package channel` (legacy from #463), NOT
`package catalog`. When moving files into `internal/catalog/`, the package
line must become `package channel`. Verify per-domain before editing:

```bash
head -1 internal/<domain>/*.go | grep -oE 'package [a-z_]+' | sort -u
```

Known: catalog→`channel`, usage→`usage`, billing→`billing`, task→`task`,
ops→`ops`, egress→`egress` (currently only subdirs), identity→`identity`,
sensitive→`sensitive`, gateway→`gateway`, security→`security`.

## §7.5 protected files — baseline SHA256 (first 12 chars)

These three must be **byte-identical** at the end. Recorded at base `bed5e0412`:

| File | Lines | sha256[0:12] |
|---|---|---|
| `internal/catalog/cache_channels.go` | 372 | `268a76d8db72` |
| `controller/relay.go` | 759 | `48c66984432a` |
| `internal/transport/middleware/distribute_channel.go` | 545 | `315121867901` |

`controller/relay.go` **moves** to `internal/gateway/` (A3), so its content hash
must be preserved across the move — only its `package` line may change.
Its `getChannel` signature at line 333 must stay:

```go
func getChannel(c contract.Context, info *relaycommon.RelayInfo, retryParam *port.SelectParams) (*model.Channel, *types.NewAPIError)
```

`SetupContextForSelectedChannel` at `distribute_channel.go:439` must stay:

```go
func SetupContextForSelectedChannel(c contract.Context, channel *model.Channel, modelName string) *types.NewAPIError
```

## §7.5 constraint 4 — RESERVED filenames, do not create

| Reserved for #454 | Domain |
|---|---|
| `internal/catalog/routestats/` (whole subpkg) | catalog |
| `internal/catalog/configure_route_stats.go` | catalog |
| `internal/catalog/select_route_unit.go` | catalog |
| `internal/catalog/store_model_routes.go` | catalog |
| `internal/catalog/sweep_route_stats.go` | catalog |
| `internal/catalog/handle_route_unit.go` | catalog |
| `internal/catalog/handle_route_unit_audit.go` | catalog |
| `internal/common/dbx/migrate_priority_weight.go` | dbx |

## Resolved open question: `service/proxy_*` → ops (not catalog)

§3 left this to be decided "by import relation, do not guess". Measured:

- `internal/ops/manage_proxy.go` is the HTTP handler; it calls
  `service.LoadProxyConfigJSON`, `service.SaveProxyConfigJSON`,
  `service.BuildSingBoxDialer`, `service.GetProxyNodeProbeStatsFor`.
- `service/proxy_*.go` does **not** import `internal/ops`. One-directional.
- `ops/manage_proxy.go:38` carries a comment saying it *duplicates*
  `service.OutboundConfig` specifically to avoid importing service.

**Verdict: `service/proxy_config.go`, `proxy_node.go`, `proxy_node_parser.go`,
`proxy_node_probe.go` → `internal/ops/`.** Merging collapses the duplicated
`OutboundConfig` type. Note `model/proxy_node.go` still goes to `egress` per §4
(it is the GORM record, different concern).

`service/channel.go` and the `codex_*` / `gateway_config_outbox` / `group` files
stay → catalog as §3 says.

## Per-batch gate (run all five, in order)

```bash
cd apps/api
GOWORK=off GOTOOLCHAIN=go1.26.4 go build ./...                     # FIRST
GOWORK=off GOTOOLCHAIN=go1.26.4 go vet ./...
GOTOOLCHAIN=go1.26.4 gofmt -l . | grep -v '^modules/'              # must be empty
GOTOOLCHAIN=go1.26.4 cpulimit -l 60 -- go test ./... -count=1 2>&1 \
  | grep -E 'FAIL:|^panic' | sed 's/ (.*//' | sort -u > /tmp/s5-bN.txt
diff /tmp/s5-base.txt /tmp/s5-bN.txt                               # must be empty
```

34th failure or any panic = regression. Stop, do not proceed.

`web/dist/index.html` stub already created (needed by `go:embed`, gitignored).
