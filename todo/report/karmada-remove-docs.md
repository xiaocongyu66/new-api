# Karmada embedded dashboard documentation removal

## Scope and codegraph evidence

- Ran `cpulimit -l 60 -- codegraph sync .` before discovery: `Synced 8 changed files`, then after deletion: `Already up to date`.
- Re-read `docs/karmada-dashboard.md`: it documented the root-only `/karmada` iframe route, HttpOnly New API session, `ROLE.SUPER_ADMIN` authorization, server-held Karmada credential, and BFF header/token handling.
- Re-read `deploy/k8s/karmada-dashboard/dashboard-embedded-session.patch`: it modifies upstream Dashboard authentication to import and apply `isEmbeddedSessionMode`, use a synthetic `__session__` token, and suppress browser token authorization headers.

## Deleted

- `docs/karmada-dashboard.md`
- `deploy/k8s/karmada-dashboard/dashboard-embedded-session.patch`

No Docker replacement guide was written; the separate deployment work owns that material.

## Retained external assets (out of scope)

- `deploy/k8s/karmada-dashboard/local.yaml`
- `deploy/k8s/karmada-dashboard/rbac.yaml`
- Karmada deployment recipes in `justfile`
- Separate deployment worktree `.wt/karmada-bootstrap` on `ops/karmada-bootstrap`

## Search and verification

- Pre-deletion path-reference search in `docs`, `deploy`, and `justfile` for `karmada-dashboard.md|dashboard-embedded-session.patch`: no matches.
- Post-deletion `git grep -n -i karmada -- docs deploy justfile` returns only `deploy/k8s/karmada-dashboard/local.yaml`, `deploy/k8s/karmada-dashboard/rbac.yaml`, and deployment recipes in `justfile`; it returns no `docs` result.
- Post-deletion `git grep -n -E 'karmada-dashboard\.md|dashboard-embedded-session\.patch' -- docs deploy justfile || true`: no output.
- `git diff --check && git diff --cached --check`: no output (clean).

## Commands

```text
cpulimit -l 60 -- codegraph sync .
git rm docs/karmada-dashboard.md deploy/k8s/karmada-dashboard/dashboard-embedded-session.patch
cpulimit -l 60 -- codegraph sync .
git diff --check
```
