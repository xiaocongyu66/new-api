# new-api Backend (`app/api`)

Go backend of the new-api gateway: Gin web framework + GORM v2, layered
Router → Controller → Service → Model. Module path:
`github.com/QuantumNous/new-api`.

- Full project conventions and backend rules: root [`AGENTS.md`](../AGENTS.md)
- Frontend embed contract: `just build-web` copies `app/web/dist` into
  `app/api/web/dist` for `//go:embed web/dist`
- Dev: `just start-api` (run) · `just dev-api` (docker compose) · `just test`
