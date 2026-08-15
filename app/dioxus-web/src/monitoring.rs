//! Monitoring tab: embeds 5 official Karmada Grafana dashboards via iframe.
//!
//! The panel fetches the Grafana base URL from GET /api/karmada/monitoring/config,
//! then renders one iframe per dashboard in kiosk mode (no Grafana nav bar).
//! A time-range selector lets the operator adjust the dashboards without leaving
//! the Karmada panel.

use dioxus::prelude::*;
use serde_json::Value;

const DASHBOARDS: &[(&str, &str)] = &[
    ("karmada-apiserver-insights", "API Server Insights"),
    ("karmada-controller-manager-insights", "Controller Manager Insights"),
    ("karmada-member-cluster-insights", "Member Cluster Insights"),
    ("karmada-scheduler-insights", "Scheduler Insights"),
    ("karmada-resource-propagation-insights", "Resource Propagation Insights"),
];

// Grafana relative-time syntax: from=now-<range>&to=now
const TIME_RANGES: &[(&str, &str)] = &[
    ("now-1h", "Last 1h"),
    ("now-6h", "Last 6h"),
    ("now-24h", "Last 24h"),
    ("now-7d", "Last 7d"),
];

#[component]
pub fn MonitoringView() -> Element {
    let mut config = use_signal(|| Value::Null);
    let mut active_subtab = use_signal(|| 0usize);
    let mut time_range_idx = use_signal(|| 0usize);
    let mut error_msg = use_signal(String::new);

    use_resource(move || async move {
        match crate::api::request("GET", "/api/karmada/monitoring/config", None).await {
            Ok(data) => config.set(data),
            Err(msg) => error_msg.set(msg),
        }
    });

    let configured = config().get("configured").and_then(Value::as_bool).unwrap_or(false);
    let grafana_url = config().get("grafanaUrl").and_then(Value::as_str).unwrap_or("").to_string();
    let time_range = TIME_RANGES.get(time_range_idx()).map(|(r, _)| *r).unwrap_or("now-1h");

    rsx! {
        div { class: "monitoring-container",
            if !error_msg().is_empty() {
                p { class: "error", "{error_msg()}" }
            }

            if !configured {
                div { class: "card",
                    h2 { "Monitoring" }
                    p { class: "muted",
                        "Grafana is not configured. Set the GRAFANA_URL environment variable "
                        "to enable dashboard embedding."
                    }
                }
            } else {
                // Sub-tab bar: one per dashboard
                div { class: "monitoring-subtabs",
                    for (i, (_, title)) in DASHBOARDS.iter().enumerate() {
                        button {
                            class: if active_subtab() == i { "subtab active" } else { "subtab" },
                            onclick: move |_| active_subtab.set(i),
                            "{title}"
                        }
                    }
                }

                // Time range selector
                div { class: "time-range-bar",
                    label { "Time range: " }
                    select {
                        class: "time-range-select",
                        value: "{time_range_idx()}",
                        onchange: move |e| {
                            if let Ok(val) = e.value().parse::<usize>() {
                                time_range_idx.set(val);
                            }
                        },
                        for (i, (_, label)) in TIME_RANGES.iter().enumerate() {
                            option { value: "{i}", selected: time_range_idx() == i, "{label}" }
                        }
                    }
                }

                // Active dashboard iframe — Grafana relative time syntax avoids
                // needing a client-side timestamp.
                { if let Some((uid, title)) = DASHBOARDS.get(active_subtab()) {
                    let src = format!(
                        "{grafana_url}/d/{uid}?kiosk=tv&from={time_range}&to=now&theme=light",
                    );
                    rsx! {
                        iframe {
                            class: "grafana-iframe",
                            src: "{src}",
                            title: "{title}",
                            allowfullscreen: "true",
                        }
                    }
                } else {
                    rsx! { p { class: "muted", "Select a dashboard." } }
                } }
            }
        }
    }
}
