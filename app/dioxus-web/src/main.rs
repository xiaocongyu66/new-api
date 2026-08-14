mod api;
mod clusters;
mod resources;

use dioxus::prelude::*;

const TABS: &[(&str, &str)] = &[
    ("clusters", "Clusters"),
    ("resources", "Resources"),
    ("policies", "Policies"),
    ("monitoring", "Monitoring"),
    ("topology", "Topology"),
    ("logs", "Logs"),
    ("alerts", "Alerts"),
];

fn main() {
    dioxus::launch(App);
}

#[component]
fn App() -> Element {
    let mut active_tab = use_signal(|| "clusters");
    let active_title = || {
        TABS.iter()
            .find(|(id, _)| *id == active_tab())
            .map(|(_, title)| *title)
            .unwrap_or("Clusters")
    };

    rsx! {
        div { class: "panel",
            nav { class: "sidebar", aria_label: "Karmada navigation",
                div { class: "brand", "Karmada Panel" }
                for (id, title) in TABS {
                    button {
                        class: if active_tab() == *id { "nav-item active" } else { "nav-item" },
                        aria_current: if active_tab() == *id { "page" } else { "false" },
                        onclick: move |_| active_tab.set(*id),
                        "{title}"
                    }
                }
            }
            main { class: "content",
                h1 { "{active_title()}" }
                if active_tab() == "clusters" {
                    clusters::ClustersView {}
                } else if active_tab() == "resources" {
                    resources::ResourcesView {}
                } else {
                    div { class: "card",
                        h2 { "Karmada Panel" }
                        p { "The {active_title()} view is ready for Karmada integration." }
                    }
                }
            }
        }
    }
}
