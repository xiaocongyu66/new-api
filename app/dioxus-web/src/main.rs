mod api;
mod clusters;
mod monitoring;
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
    setup_theme_listener();
    dioxus::launch(App);
}

fn setup_theme_listener() {
    let script = r#"
        var ALLOWED = ['--font-body','--background','--foreground','--card','--card-foreground','--muted','--muted-foreground','--accent','--accent-foreground','--border','--sidebar','--sidebar-foreground','--sidebar-accent','--sidebar-accent-foreground','--sidebar-border','--sidebar-ring','--radius'];
        window.addEventListener('message', function(e) {
            if (e.origin !== window.location.origin) return;
            var d = e.data;
            if (!d || d.type !== 'theme') return;
            var root = document.documentElement;
            root.setAttribute('data-theme', d.theme || 'dark');
            if (d.tokens) {
                Object.keys(d.tokens).forEach(function(k) {
                    if (ALLOWED.indexOf(k) !== -1) {
                        root.style.setProperty(k, d.tokens[k]);
                    }
                });
            }
        });
        var params = new URLSearchParams(window.location.search);
        var initial = params.get('theme');
        if (initial) {
            document.documentElement.setAttribute('data-theme', initial);
        }
    "#;
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
                } else if active_tab() == "monitoring" {
                    monitoring::MonitoringView {}
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
