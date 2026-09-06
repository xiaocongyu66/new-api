---
name: newapi-production-upgrade
description: >-
  Production upgrade, database migration, and web server transition manual for new-api
  on standalone production servers (e.g. your-domain.example.com). Covers safe
  PostgreSQL 17->18 migration, TimescaleDB hypertable conversion, GORM AutoMigrate
  constraint traps, Caddy with replace-response plugin replacing Nginx, and GitHub
  Actions automated deployment secrets configuration.
---

# new-api Production Upgrade & Infrastructure Migration Guide

This skill records the hard-won production upgrade procedures, database migration contracts, GORM pitfalls, and Caddy reverse proxy configurations for `new-api`. Use this skill whenever planning or executing upgrades on standalone production instances.

## 1. SSH Deployment Secrets Architecture

### Key Pair Roles (Client vs. Server)
In SSH automated deployments:
- **Client (GitHub Actions Runner)**: Initiates the connection; **MUST hold the Private Key** (`PROD_SERVER_SSH_KEY`) to generate cryptographic signatures.
- **Server (Production Host `your-server-ip`)**: Accepts the connection; **MUST hold the Public Key** in `~/.ssh/authorized_keys` to verify client signatures.
- *Caution*: Storing the public key in GitHub Secrets will fail with `Permission denied (publickey)` because the client cannot sign the challenge without the private key.

### GitHub Secrets Specification
Configure the following secrets in the repository under `Settings -> Secrets and variables -> Actions`:

| Secret Name | Required | Example | Description |
|---|---|---|---|
| `PROD_SERVER_HOST` | **Yes** | `your-server-ip` | Production server IP or resolvable hostname. |
| `PROD_SERVER_USER` | **Yes** | `root` | SSH login username. |
| `PROD_SERVER_SSH_KEY` | **Yes** | `-----BEGIN OPENSSH...` | Dedicated ED25519 private key (injected via pipe without displaying). |
| `PROD_SERVER_SSH_PORT` | No | `22` | SSH port (defaults to `22`). |
| `PROD_DEPLOY_PATH` | No | `/opt/new-api/deploy` | Path containing `docker-compose.yml` (defaults to `/opt/new-api/deploy`). |

*Important*: Never confuse these with `SERVERS_LIST_JSON`, which is reserved exclusively for unstable testing nodes and K3s cluster maintenance.

---

## 2. GORM AutoMigrate Critical Pitfalls

### The `prefill_groups` Constraint Crash (SQLSTATE 42704)
- **Root Cause**: Older production databases created `prefill_groups` with a unique constraint named `idx_prefill_groups_name UNIQUE (name)`. When the Go model was updated to support soft deletion (`uniqueIndex:uk_prefill_name,where:deleted_at IS NULL`), GORM's PostgreSQL migrator detects that `field.Unique` is false and attempts to drop the old unique constraint.
- **The Trap**: GORM hardcodes its default generated constraint name `uni_prefill_groups_name` without `IF EXISTS`. Because the actual constraint is named `idx_prefill_groups_name`, PostgreSQL throws:
  ```text
  ERROR: constraint "uni_prefill_groups_name" of relation "prefill_groups" does not exist (SQLSTATE 42704)
  ```
  This causes an immediate fatal crash on startup.
- **Contract Rule**: Any pre-existing unique constraint that diverges from GORM's naming convention must be dropped or reconciled **before** GORM `AutoMigrate` runs.
- **Implementation Anchor**: In `apps/api/internal/dbinfra/open_db.go`:
  ```go
  func migratePrefillGroupConstraint() error {
      if !common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
          return nil
      }
      var exists bool
      if err := dbx.DB.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'prefill_groups')").Scan(&exists).Error; err != nil {
          return err
      }
      if !exists {
          return nil
      }
      return dbx.DB.Exec("ALTER TABLE prefill_groups DROP CONSTRAINT IF EXISTS idx_prefill_groups_name").Error
  }
  ```

### The `logs` Partition Column NOT NULL Constraint (SQLSTATE TS101)
- **Root Cause**: TimescaleDB enforces that the time-partitioning column (`created_at`) is `NOT NULL`. Without an explicit `not null` tag on `Log.CreatedAt`, GORM `AutoMigrate` observes that the Go struct does not declare `not null` and emits:
  ```sql
  ALTER TABLE "logs" ALTER COLUMN "created_at" DROP NOT NULL;
  ```
  TimescaleDB rejects dropping `NOT NULL` on a partitioned column:
  ```text
  ERROR: cannot drop not-null constraint from a time-partitioned column (SQLSTATE TS101)
  ```
  This causes a crash on the second restart.
- **Contract Rule**: In `apps/api/internal/usage/store_log.go`, `CreatedAt` must include `gorm:"...;not null;..."`.

---

## 3. Database Migration: PostgreSQL 17 -> 18 + TimescaleDB

### Volume Mount Convention Change
- PostgreSQL 17: volume mounted to `/var/lib/postgresql/data`.
- PostgreSQL 18+: volume mounted to `/var/lib/postgresql` (the container creates `data/` as a subdirectory to allow `pg_upgrade --link`).
- Never reuse a PG17 volume path directly with a PG18 container.

### In-Place Hypertable Conversion Sequence
To convert an existing plain `logs` table into a TimescaleDB hypertable without data loss:
```sql
BEGIN;
-- 1. Ensure partition column is NOT NULL
ALTER TABLE logs ALTER COLUMN created_at SET NOT NULL;

-- 2. TimescaleDB requires the partition column in all unique indexes
ALTER TABLE logs DROP CONSTRAINT IF EXISTS logs_pkey;
ALTER TABLE logs ADD PRIMARY KEY (created_at, id);
CREATE INDEX IF NOT EXISTS idx_logs_id ON logs (id);

-- 3. Convert table and migrate existing rows into chunks
SELECT create_hypertable(
    'logs', 'created_at',
    chunk_time_interval => 86400::bigint,
    migrate_data => TRUE,
    if_not_exists => TRUE
);

-- 4. Register integer_now for the bigint Unix epoch column
CREATE OR REPLACE FUNCTION logs_created_at_now() RETURNS bigint
    LANGUAGE SQL STABLE AS $$ SELECT extract(epoch FROM now())::bigint $$;
PERFORM set_integer_now_func('logs', 'logs_created_at_now', replace_if_exists => TRUE);
COMMIT;
```

### Performance & Memory Budget
- **Dump time (`pg_dump -Fc`)**: ~0.6s for 125k rows.
- **Restore time (`pg_restore`)**: ~1.6s.
- **Conversion time (125k rows / 15 indexes)**: ~3.1s.
- **Peak memory during conversion**: ~216 MiB (PostgreSQL) + ~182 MiB (new-api).
- **Post-restore mandatory step**: Run `ANALYZE logs;` immediately to refresh query planner statistics.

---

## 4. Web Server Transition: Caddy Replacing Nginx

### Key Directives Mapping
| Feature | Nginx Implementation | Caddyfile Equivalent |
|---|---|---|
| Cloudflare Origin SSL | `ssl_certificate /etc/nginx/ssl/...` | `tls /etc/ssl/cert.crt /etc/ssl/cert.key` |
| Cloudflare Real-IP | `set_real_ip_from ...; real_ip_header ...` | `trusted_proxies static ...; client_ip_headers CF-Connecting-IP` |
| SSE Streaming (No Buffering) | `proxy_buffering off;` | `reverse_proxy { flush_interval -1 }` |
| Route Prefix Stripping | `rewrite ^/grok(/.*)$ $1 break; proxy_pass ...` | `handle_path /grok/* { reverse_proxy 127.0.0.1:18000 }` |
| Static Files | `try_files /robots.txt =404;` | `file_server` inside `handle @seo_static` |
| SPA SEO Tag Rewriting | `sub_filter '<title>...' '<title>...'` | `replace { '<title>...' '<title>...' }` via `replace-response` |

### Critical Caddyfile Rules
1. **Global Options Order**: Because `replace` is a third-party plugin, `order replace after encode` must be declared in the global block `{ ... }`.
2. **Directive Placement**: The `replace` block must be placed at the `handle` level alongside `reverse_proxy`, **never** nested inside `reverse_proxy`.
3. **Upstream Compression**: Add `header_up Accept-Encoding identity` to `reverse_proxy` so the backend does not gzip responses before `replace` can modify the body.
4. **Build Proxy Requirement**: When building custom Caddy images with `xcaddy`, always specify `ENV GOPROXY=https://goproxy.cn,direct` in Dockerfile to prevent timeouts on `proxy.golang.org`.

---

## 5. Rollback Playbook (Under 10 Seconds)

Never delete old containers (`docker rm`) during upgrades. Rename and freeze them:
```bash
# Freeze old PG container
docker rename newapi-postgres newapi-postgres-v17-bak
docker update --restart=no newapi-postgres-v17-bak
docker stop newapi-postgres-v17-bak

# Freeze old new-api container
docker rename new-api new-api-v17-bak
docker update --restart=no new-api-v17-bak
docker stop new-api-v17-bak
```

### Instant Revert:
```bash
# 1. Revert Web Server
docker stop nailao-caddy && systemctl start nginx && systemctl enable nginx

# 2. Revert Application & Database
cd /opt/new-api/deploy
docker stop new-api newapi-postgres 2>/dev/null || true
docker update --restart=always newapi-postgres-v17-bak && docker start newapi-postgres-v17-bak && docker rename newapi-postgres-v17-bak newapi-postgres
cp docker-compose.yml.bak-v17 docker-compose.yml
docker update --restart=always new-api-v17-bak && docker start new-api-v17-bak && docker rename new-api-v17-bak new-api
```
