use dioxus::prelude::*;

/// Root application component.
/// Wires up providers, router, and global state.
#[component]
pub fn RootApp() -> Element {
    rsx! {
        // Phase 2: Provider nesting (theme, auth, layout, font, direction)
        // Router { Routes {} }

        // Phase 0: Hello World placeholder
        div {
            class: "flex h-screen items-center justify-center text-2xl font-bold",
            "New API — Dioxus frontend (web-rs)"
        }
    }
}
