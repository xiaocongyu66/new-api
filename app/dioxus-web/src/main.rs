mod api;
mod clusters;

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
        window.addEventListener('message', function(e) {
            if (e.origin !== window.location.origin) return;
            var d = e.data;
            if (!d || d.type !== 'theme') return;
            var root = document.documentElement;
            root.setAttribute('data-theme', d.theme || 'dark');
            if (d.tokens) {
                Object.keys(d.tokens).forEach(function(k) {
                    root.style.setProperty(k, d.tokens[k]);
                });
            }
        });
        var params = new URLSearchParams(window.location.search);
        var initial = params.get('theme');
        if (initial) {
            document.documentElement.setAttribute('data-theme', initial);
        }
    "#;
    let _ = dioxus::document::eval(script);
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
