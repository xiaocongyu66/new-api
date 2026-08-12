# Dioxus Web Build

The pilot Karmada panel lives under `app/dioxus-web/` and builds as a static web bundle. The generated files are copied into `app/api/web/dist/dioxus/`, which is embedded by the Go API binary.

```bash
./scripts/build-dioxus-web.sh
```

The Go server serves `/dioxus/` as static assets. The React route `/karmada` is the authenticated super-admin entry point and embeds `/dioxus/` without changing the existing React application shell.

The script requires the Dioxus CLI (`dx`), the `wasm32-unknown-unknown` Rust target, and a Rust toolchain. It intentionally does not commit generated files under `app/api/web/dist/`; CI and release builds generate them before compiling the Go binary.
