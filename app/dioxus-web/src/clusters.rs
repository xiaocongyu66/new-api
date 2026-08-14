//! Cluster overview tab: member cluster health, capacity and sync latency.

use dioxus::prelude::*;
use serde_json::Value;

use crate::api;

/// Renders the member cluster table and, when a row is selected, its detail
/// panel. Detail data is fetched on demand so the list stays cheap.
#[component]
pub fn ClustersView() -> Element {
    let mut selected = use_signal(|| Option::<String>::None);
    let clusters =
        use_resource(|| async move { api::request("GET", "/api/karmada/clusters", None).await });

    let detail = use_resource(move || async move {
        let name = selected()?;
        Some(
            api::request(
                "GET",
                &format!("/api/karmada/clusters/{}", urlencode(&name)),
                None,
            )
            .await,
        )
    });

    rsx! {
        div { class: "stack",
            match &*clusters.read_unchecked() {
                None => rsx! { p { class: "muted", "Loading member clusters…" } },
                Some(Err(message)) => rsx! { p { class: "error", "{message}" } },
                Some(Ok(data)) => {
                    let items = api::list_field(data, "clusters");
                    if items.is_empty() {
                        rsx! { p { class: "muted", "No member cluster is registered in Karmada." } }
                    } else {
                        rsx! {
                            div { class: "table-scroll",
                                table { class: "grid",
                                    thead {
                                        tr {
                                            th { "Cluster" }
                                            th { "Status" }
                                            th { "API server" }
                                            th { "Version" }
                                            th { "Nodes" }
                                            th { "CPU" }
                                            th { "Memory" }
                                            th { "Sync P95" }
                                        }
                                    }
                                    tbody {
                                        for cluster in items {
                                            ClusterRow {
                                                key: "{api::text(&cluster, \"name\")}",
                                                cluster: cluster.clone(),
                                                on_select: move |name: String| selected.set(Some(name)),
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            match (detail.pending(), &*detail.read_unchecked()) {
                (true, _) | (false, None) | (false, Some(None)) => {
                    rsx! { p { class: "muted", "Loading cluster detail…" } }
                }
                (false, Some(Some(Err(message)))) => {
                    rsx! { p { class: "error", "{message}" } }
                }
                (false, Some(Some(Ok(data)))) => {
                    rsx! { ClusterDetailPanel { detail: data.clone() } }
                }
            }
        }
    }
}

/// One member cluster row. Clicking the row loads its detail panel.
#[component]
fn ClusterRow(cluster: Value, on_select: EventHandler<String>) -> Element {
    let name = api::text(&cluster, "name");
    let status = api::text(&cluster, "status");
    let status_class = match status.as_str() {
        "Ready" => "pill ready",
        "NotReady" => "pill not-ready",
        _ => "pill unknown",
    };
    rsx! {
        tr { class: "row-clickable",
            onclick: move |_| on_select.call(name.clone()),
            td {
                button { class: "link", "{name}" }
            }
            td {
                span { class: "{status_class}", "{status}" }
            }
            td { class: "mono", "{api::text(&cluster, \"api_endpoint\")}" }
            td { "{api::text(&cluster, \"version\")}" }
            td { "{api::int(&cluster, \"ready_nodes\")} / {api::int(&cluster, \"total_nodes\")}" }
            td { "{api::metric(&cluster, \"cpu_percent\", \"%\")}" }
            td { "{api::metric(&cluster, \"memory_percent\", \"%\")}" }
            td { "{api::duration_metric(&cluster, \"sync_p95_seconds\")}" }
        }
    }
}

/// Detail panel: resource counts, node list and recent events for one cluster.
#[component]
fn ClusterDetailPanel(detail: Value) -> Element {
    let warnings = api::list_field(&detail, "warnings");
    let nodes = api::list_field(&detail, "nodes");
    let events = api::list_field(&detail, "events");
    rsx! {
        section { class: "card",
            h2 { "{api::text(&detail, \"name\")}" }
            p { class: "muted",
                "Sync mode {api::text(&detail, \"sync_mode\")} · Kubernetes {api::text(&detail, \"version\")}"
            }

            for warning in warnings {
                p { class: "error", "{warning.as_str().unwrap_or_default()}" }
            }

            div { class: "counts",
                Count { label: "Deployments", value: api::int(&detail, "deployments") }
                Count { label: "Pods", value: api::int(&detail, "pods") }
                Count { label: "Services", value: api::int(&detail, "services") }
            }
            if detail.get("truncated").and_then(Value::as_bool) == Some(true) {
                p { class: "muted", "Counts are a partial page returned by the member cluster." }
            }

            h3 { "Nodes" }
            if nodes.is_empty() {
                p { class: "muted", "No node data available." }
            } else {
                div { class: "table-scroll",
                    table { class: "grid",
                        thead {
                            tr {
                                th { "Node" }
                                th { "Status" }
                                th { "Kubelet" }
                            }
                        }
                        tbody {
                            for node in nodes {
                                tr {
                                    td { "{api::text(&node, \"name\")}" }
                                    td { "{api::text(&node, \"status\")}" }
                                    td { "{api::text(&node, \"version\")}" }
                                }
                            }
                        }
                    }
                }
            }

            h3 { "Recent events" }
            if events.is_empty() {
                p { class: "muted", "No recent event was reported." }
            } else {
                ul { class: "events",
                    for event in events {
                        li {
                            span { class: "mono", "{api::text(&event, \"timestamp\")}" }
                            " "
                            strong { "{api::text(&event, \"type\")}/{api::text(&event, \"reason\")}" }
                            " "
                            span { class: "mono", "{api::text(&event, \"object\")}" }
                            div { class: "muted", "{api::text(&event, \"message\")}" }
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn Count(label: String, value: i64) -> Element {
    rsx! {
        div { class: "count",
            div { class: "count-value", "{value}" }
            div { class: "muted", "{label}" }
        }
    }
}

/// Percent-encodes a path segment. Cluster names are DNS labels in practice,
/// but the panel must not build a broken URL if one ever contains `/`.
pub fn urlencode(value: &str) -> String {
    let mut encoded = String::with_capacity(value.len());
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                encoded.push(byte as char)
            }
            _ => encoded.push_str(&format!("%{byte:02X}")),
        }
    }
    encoded
}
