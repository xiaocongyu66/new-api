# Karmada locale translation removal

## Scope and codegraph

- Ran `cpulimit -l 60 -- codegraph sync .` from the assigned worktree; the graph was already up to date.
- Searched `apps/web/src` for the three target keys plus `VITE_KARMADA_DASHBOARD_URL`; no source usages remained before or after this change.
- The only matching entries before removal were the three specified keys in each of the seven locale files. The former `Karmada Dashboard is not configured` entry was deliberately retained because it was outside the requested deletion set.

## Sanctioned locale workflow

`apps/web/scripts/add-missing-keys.mjs` was absent, so I created the smallest temporary script that declared the three keys in a `deletedKeys` set and iterated the fixed seven-locale list. It parsed, deleted only present keys, and wrote each locale. The temporary script was removed after execution; no reusable deletion API was committed.

Deletion output:

```text
en: 3 translations deleted
zh: 3 translations deleted
zh-TW: 3 translations deleted
fr: 3 translations deleted
ja: 3 translations deleted
ru: 3 translations deleted
vi: 3 translations deleted

Total: 21 translations deleted
```

## Sync report and verification

- Ran `cd apps/web && cpulimit -l 60 -- bun run i18n:sync`.
- `_reports/_sync-report.json` reports `missingCount: 0`, `extrasCount: 0`, and `untranslatedCount: 0` for en, fr, ja, ru, vi, zh-TW, and zh.
- Searched all locale JSON files for each removed key: no matches.
- Ran `cd apps/web && cpulimit -l 60 -- bun run typecheck`: passed (`tsgo -b`).
- Ran `cd apps/web && cpulimit -l 60 -- bun run build`: passed (`Rsbuild v2.1.6`, `ready built in 9.23s`).

The locale diff removes exactly the three requested Karmada translations in each locale (21 removals total). Sync also legitimately refreshed `_sync-report.json` (all previous seven extras cleared) and removed the three now-empty Karmada untranslated reports for ja, ru, and zh.
