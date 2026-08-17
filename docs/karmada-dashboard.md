# Karmada Dashboard Integration

The Karmada entry in the New API Admin workspace is a protected iframe host for the official [Karmada Dashboard](https://github.com/karmada-io/dashboard). New API does not copy the Dashboard source code into this repository and does not proxy or expose Karmada service-account credentials.

## Root-only embedded session

The embedded route uses a New API HttpOnly session cookie and is restricted to an active `ROLE.SUPER_ADMIN` browser session. It does not send or store a Karmada bearer token in the iframe, URL, browser storage, or `postMessage` payload.

Configure the New API server with these server-only values:

```bash
KARMADA_DASHBOARD_URL=https://dashboard-web.internal/karmada-dashboard/
KARMADA_DASHBOARD_TOKEN=<server-held-karmada-credential>
```

Build the official Dashboard UI in embedded session mode with the parent origin for the intended deployment:

```bash
VITE_KARMADA_EMBEDDED_SESSION_MODE=true \
VITE_KARMADA_PARENT_ORIGIN=https://new-api.example.com \
pnpm run dashboard:build
```

The BFF validates the New API login session and current root role for every proxied Dashboard request, strips browser-provided authorization and impersonation headers, injects the Karmada credential only for upstream API requests, and does not forward upstream cookies back to the browser. Revoking the New API session, disabling the account, or removing root access immediately prevents further Dashboard requests.

## Deployment contract

Deploy the official Karmada Dashboard web and API components using its Kubernetes manifests or Helm chart. The web service must be reachable at the URL supplied to the Web build as `VITE_KARMADA_DASHBOARD_URL`. The default value is the same-origin path `/karmada-dashboard/`.

### Local Kind/Karmada workflow

For an existing Karmada host cluster, the repository supplies a low-resource local overlay and `just` tasks:

```bash
just karmada-dashboard-local
just karmada-dashboard-forward
just karmada-dashboard-token
```

The overlay uses three single-replica official Dashboard roles: the public Web proxy, the Karmada API, and the member-cluster API. It explicitly disables metrics scraping to avoid a fourth workload. These roles cannot safely be collapsed into two Pods because the Web proxy and both API servers are independently addressable processes with separate lifecycle and security boundaries. A real Karmada control plane and member cluster are prerequisites; plain K3s/Kubernetes is not a compatible substitute.

`karmada-dashboard-forward` exposes the official Web UI at `http://localhost:18000/karmada-dashboard/`. Set that value in `VITE_KARMADA_DASHBOARD_URL` when running the New API Web development server. `karmada-dashboard-clean` removes only the local Dashboard resources and its credentials, not the Karmada control plane.

For a same-origin path deployment, configure the official Dashboard `path_prefix` to `/karmada-dashboard` and route all of these paths to its web service:

- `/karmada-dashboard/` and SPA fallback routes
- `/karmada-dashboard/static/`
- `/karmada-dashboard/api/`
- `/karmada-dashboard/clusterapi/`
- `/karmada-dashboard/metrics-scraper/`
- WebSocket requests used by the terminal feature

The official Dashboard owns its JWT/OIDC login and stores its token in its own browser storage. Cross-origin iframes cannot share that storage, so a separately hosted Dashboard will show its own login page. Do not put a long-lived Karmada service-account token in `VITE_KARMADA_DASHBOARD_URL`, a query string, or New API frontend code.

## Access control

The New API route and sidebar entry are restricted to `ROLE.SUPER_ADMIN`. This protects access to the integration entry only; the official Dashboard and its Karmada permissions remain responsible for authenticating and authorizing operations inside the iframe.

## Development

For local development, set the URL when starting the Web app, for example:

```bash
VITE_KARMADA_DASHBOARD_URL=http://localhost:18000/karmada-dashboard/ bun run dev -- --host 0.0.0.0 --port 5173
```

The official Dashboard still requires its own API/web process and a valid Karmada kubeconfig. A Vite static preview without the official Dashboard web proxy is not sufficient for authenticated API access.
