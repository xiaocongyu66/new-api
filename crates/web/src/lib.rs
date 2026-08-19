pub mod app;
pub mod components;
pub mod context;
pub mod hooks;
pub mod api;
pub mod types;
pub mod i18n;
pub mod routes;
pub mod features;
pub mod stores;
pub mod utils;

use dioxus::prelude::*;

/// Root app entry point.
/// Called by `apps/web-rs/src/main.rs` via `dioxus::launch(newapi_web::App)`.
#[allow(non_snake_case)]
pub fn App() -> Element {
    rsx! {
        app::RootApp {}
    }
}
