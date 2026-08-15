//! Resource topology tab: server-built Karmada propagation graph rendered with native SVG.

use dioxus::prelude::*;
use serde_json::Value;

use crate::{api, clusters::urlencode};

const RESOURCE_TYPES: &[&str] = &["Deployment", "StatefulSet", "DaemonSet"];

#[derive(Clone, PartialEq)]
struct TopologyNode {
    id: String,
    node_type: String,
    name: String,
    namespace: String,
    cluster: String,
    status: String,
    metadata: Value,
}

#[derive(Clone, PartialEq)]
struct TopologyEdge {
    from: String,
    to: String,
}

#[component]
pub fn TopologyView() -> Element {
    let mut namespace = use_signal(|| "default".to_string());
    let mut cluster = use_signal(String::new);
    let mut resource_type = use_signal(|| "Deployment".to_string());
    let mut refresh = use_signal(|| 0u32);
    let mut selected = use_signal(|| Option::<TopologyNode>::None);
    let mut scale = use_signal(|| 1.0f64);
    let mut offset_x = use_signal(|| 0.0f64);
    let mut offset_y = use_signal(|| 0.0f64);
    let mut drag_start = use_signal(|| Option::<(f64, f64, f64, f64)>::None);

    let clusters =
        use_resource(|| async move { api::request("GET", "/api/karmada/clusters", None).await });
    let topology = use_resource(move || {
        let namespace = namespace();
        let cluster = cluster();
        let resource_type = resource_type();
        let _refresh = refresh();
        async move {
            api::request(
                "GET",
                &topology_path(&namespace, &cluster, &resource_type),
                None,
            )
            .await
        }
    });

    let zoom_in = move |_| scale.set((scale() + 0.15).min(1.8));
    let zoom_out = move |_| scale.set((scale() - 0.15).max(0.55));
    let reset_view = move |_| {
        scale.set(1.0);
        offset_x.set(0.0);
        offset_y.set(0.0);
    };

    rsx! {
        div { class: "topology-shell stack",
            section { class: "card stack topology-intro",
                div { class: "section-heading",
                    div {
                        p { class: "eyebrow", "Propagation trace" }
                        h2 { "Resource topology" }
                        p { class: "muted", "Trace a bounded Policy → Binding → Work → Deployment → Pod path for one namespace and optional member cluster." }
                    }
                    button {
                        class: "button secondary",
                        onclick: move |_| {
                            selected.set(None);
                            refresh.set(refresh() + 1);
                        },
                        "Refresh"
                    }
                }
                div { class: "toolbar topology-filters",
                    label { class: "field",
                        span { "Namespace" }
                        input {
                            class: "text-input",
                            value: "{namespace()}",
                            placeholder: "default",
                            oninput: move |event| {
                                namespace.set(event.value());
                                selected.set(None);
                            }
                        }
                    }
                    label { class: "field",
                        span { "Member cluster" }
                        select {
                            value: "{cluster()}",
                            onchange: move |event| {
                                cluster.set(event.value());
                                selected.set(None);
                            },
                            option { value: "", "All bound clusters" }
                            match &*clusters.read_unchecked() {
                                Some(Ok(data)) => rsx! {
                                    for item in api::list_field(data, "clusters") {
                                        option { value: "{api::text(&item, \"name\")}", "{api::text(&item, \"name\")}" }
                                    }
                                },
                                _ => rsx! {},
                            }
                        }
                    }
                    label { class: "field",
                        span { "Resource type" }
                        select {
                            value: "{resource_type()}",
                            onchange: move |event| {
                                resource_type.set(event.value());
                                selected.set(None);
                            },
                            for option in RESOURCE_TYPES {
                                option { value: "{option}", "{option}" }
                            }
                        }
                    }
                }
                p { class: "notice", "Performance: the API requests one namespace/type slice, limits every list to 50 items, and only follows the selected cluster when set." }
            }

            match &*topology.read_unchecked() {
                None => rsx! { section { class: "card", p { class: "muted", "Loading topology…" } } },
                Some(Err(message)) => rsx! { section { class: "card", p { class: "error", "{message}" } } },
                Some(Ok(data)) => {
                    let (nodes, edges) = graph_data(data);
                    if nodes.is_empty() {
                        rsx! { section { class: "card empty-panel", "No propagation chain matched the selected filters." } }
                    } else {
                        let transform = format!("translate({} {}) scale({})", offset_x(), offset_y(), scale());
                        rsx! {
                            section { class: "card topology-card",
                                div { class: "topology-toolbar",
                                    div { class: "topology-legend", aria_label: "Topology status legend",
                                        Legend { class: "healthy", label: "Healthy" }
                                        Legend { class: "syncing", label: "Syncing" }
                                        Legend { class: "failed", label: "Failed" }
                                    }
                                    div { class: "topology-controls", aria_label: "Topology view controls",
                                        button { class: "icon-button", aria_label: "Zoom out", onclick: zoom_out, "−" }
                                        span { class: "zoom-value", "{(scale() * 100.0).round() as i32}%" }
                                        button { class: "icon-button", aria_label: "Zoom in", onclick: zoom_in, "+" }
                                        button { class: "button secondary", onclick: reset_view, "Reset view" }
                                    }
                                }
                                div {
                                    class: "topology-canvas",
                                    role: "application",
                                    aria_label: "Interactive resource topology; drag the background to pan, use zoom controls to scale",
                                    onpointerdown: move |event| {
                                        let point = event.client_coordinates();
                                        drag_start.set(Some((point.x, point.y, offset_x(), offset_y())));
                                    },
                                    onpointermove: move |event| {
                                        if let Some((start_x, start_y, base_x, base_y)) = drag_start() {
                                            let point = event.client_coordinates();
                                            offset_x.set(base_x + point.x - start_x);
                                            offset_y.set(base_y + point.y - start_y);
                                        }
                                    },
                                    onpointerup: move |_| drag_start.set(None),
                                    onpointercancel: move |_| drag_start.set(None),
                                    TopologyGraph {
                                        nodes: nodes.clone(),
                                        edges: edges.clone(),
                                        mobile: false,
                                        transform: transform.clone(),
                                        on_select: move |node: TopologyNode| selected.set(Some(node))
                                    }
                                    TopologyGraph {
                                        nodes,
                                        edges,
                                        mobile: true,
                                        transform,
                                        on_select: move |node: TopologyNode| selected.set(Some(node))
                                    }
                                }
                            }
                        }
                    }
                }
            }

            if let Some(node) = selected() {
                TopologyDrawer { node, on_close: move |_| selected.set(None) }
            }
        }
    }
}

#[component]
fn TopologyGraph(
    nodes: Vec<TopologyNode>,
    edges: Vec<TopologyEdge>,
    mobile: bool,
    transform: String,
    on_select: EventHandler<TopologyNode>,
) -> Element {
    let class = if mobile {
        "topology-graph topology-graph-mobile"
    } else {
        "topology-graph topology-graph-desktop"
    };
    let marker_id = if mobile {
        "topology-arrow-mobile"
    } else {
        "topology-arrow-desktop"
    };
    let marker_url = format!("url(#{marker_id})");
    let view_box = if mobile {
        format!("0 0 280 {}", mobile_graph_height(nodes.len()))
    } else {
        "0 0 1160 680".to_string()
    };

    rsx! {
        svg {
            class: "{class}",
            view_box: "{view_box}",
            "aria-label": "Karmada propagation topology graph",
            defs {
                marker {
                    id: "{marker_id}",
                    marker_width: "8",
                    marker_height: "8",
                    ref_x: "7",
                    ref_y: "4",
                    orient: "auto",
                    path { d: "M 0 0 L 8 4 L 0 8 z", class: "topology-arrow" }
                }
            }
            g { transform: "{transform}",
                for (x1, y1, x2, y2) in edges.iter().filter_map(|edge| {
                    Some((
                        node_position(&nodes, &edge.from, mobile)?,
                        node_position(&nodes, &edge.to, mobile)?,
                    ))
                }).map(|(from, to)| edge_coordinates(from, to, mobile)) {
                    line {
                        class: "topology-edge",
                        x1: "{x1}", y1: "{y1}",
                        x2: "{x2}", y2: "{y2}",
                        marker_end: "{marker_url}"
                    }
                }
                for (node, x, y, class, aria_label) in nodes.iter().enumerate().map(|(index, node)| {
                    let (x, y) = graph_position(index, &node.node_type, mobile);
                    let class = format!("topology-node {}", status_class(&node.status));
                    let aria_label = format!("Open {} {} details", node.node_type, node.name);
                    (node.clone(), x, y, class, aria_label)
                }) {
                    g {
                        class: "{class}",
                        transform: "translate({x} {y})",
                        tabindex: "0",
                        role: "button",
                        "aria-label": "{aria_label}",
                        onclick: move |_| on_select.call(node.clone()),
                        rect { width: "178", height: "88", rx: "10" }
                        text { class: "topology-node-type", x: "14", y: "27", "{node.node_type}" }
                        text { class: "topology-node-name", x: "14", y: "52", "{truncate(&node.name, 20)}" }
                        text { class: "topology-node-scope", x: "14", y: "72", "{node_scope(&node)}" }
                    }
                }
            }
        }
    }
}

#[component]
fn Legend(class: &'static str, label: &'static str) -> Element {
    rsx! { span { class: "legend-item", span { class: "legend-dot {class}" }, "{label}" } }
}

#[component]
fn TopologyDrawer(node: TopologyNode, on_close: EventHandler<()>) -> Element {
    let metadata =
        serde_json::to_string_pretty(&node.metadata).unwrap_or_else(|_| "{}".to_string());
    rsx! {
        div { class: "policy-drawer-layer", role: "presentation",
            button { class: "drawer-scrim", aria_label: "Close resource metadata", onclick: move |_| on_close.call(()) }
            aside { class: "policy-drawer topology-drawer", role: "dialog", aria_modal: "true", aria_label: "Topology node details",
                div { class: "section-heading",
                    div {
                        p { class: "eyebrow", "{node.node_type}" }
                        h2 { "{node.name}" }
                        p { class: "muted", "{node_scope(&node)}" }
                    }
                    button { class: "drawer-close", aria_label: "Close topology details", onclick: move |_| on_close.call(()), "×" }
                }
                div { class: "topology-detail-status {status_class(&node.status)}", "{node.status}" }
                h3 { "Metadata" }
                pre { class: "topology-metadata", "{metadata}" }
            }
        }
    }
}

fn topology_path(namespace: &str, cluster: &str, resource_type: &str) -> String {
    let mut path = format!(
        "/api/karmada/topology?namespace={}&kind={}",
        urlencode(namespace.trim()),
        urlencode(resource_type),
    );
    if !cluster.is_empty() {
        path.push_str("&cluster=");
        path.push_str(&urlencode(cluster));
    }
    path
}

fn graph_data(data: &Value) -> (Vec<TopologyNode>, Vec<TopologyEdge>) {
    let nodes = api::list_field(data, "nodes")
        .into_iter()
        .map(|item| TopologyNode {
            id: api::text(&item, "id"),
            node_type: api::text(&item, "type"),
            name: api::text(&item, "name"),
            namespace: api::text(&item, "namespace"),
            cluster: api::text(&item, "cluster"),
            status: api::text(&item, "status"),
            metadata: item.get("metadata").cloned().unwrap_or(Value::Null),
        })
        .filter(|node| !node.id.is_empty())
        .collect();
    let edges = api::list_field(data, "edges")
        .into_iter()
        .map(|item| TopologyEdge {
            from: api::text(&item, "from"),
            to: api::text(&item, "to"),
        })
        .filter(|edge| !edge.from.is_empty() && !edge.to.is_empty())
        .collect();
    (nodes, edges)
}

fn graph_position(index: usize, node_type: &str, mobile: bool) -> (i32, i32) {
    if mobile {
        return (31, 30 + index as i32 * 120);
    }
    let column = match node_type {
        "PropagationPolicy" => 0,
        "ResourceBinding" => 1,
        "Work" => 2,
        "Deployment" | "StatefulSet" | "DaemonSet" => 3,
        "Pod" => 4,
        _ => 5,
    };
    let row = index % 4;
    (30 + column * 190, 80 + row as i32 * 132)
}

fn mobile_graph_height(node_count: usize) -> i32 {
    (30 + node_count.max(1) as i32 * 120 + 58).max(640)
}

fn edge_coordinates(from: (i32, i32), to: (i32, i32), mobile: bool) -> (i32, i32, i32, i32) {
    if mobile {
        (from.0 + 89, from.1 + 88, to.0 + 89, to.1 - 10)
    } else {
        (from.0 + 178, from.1 + 44, to.0 - 10, to.1 + 44)
    }
}

#[cfg(test)]
mod tests {
    use super::{graph_position, mobile_graph_height};

    #[test]
    fn keeps_desktop_and_mobile_chains_inside_their_viewboxes() {
        assert_eq!(graph_position(0, "Deployment", false).0, 600);
        assert_eq!(graph_position(0, "StatefulSet", false).0, 600);
        assert_eq!(graph_position(0, "DaemonSet", false).0, 600);
        assert_eq!(graph_position(0, "Pod", false).0, 790);
        assert!(graph_position(0, "Pod", false).0 + 178 <= 1160);
        assert_eq!(graph_position(0, "PropagationPolicy", true), (31, 30));
        assert_eq!(graph_position(1, "ResourceBinding", true), (31, 150));
        assert_eq!(graph_position(4, "Pod", true), (31, 510));
        assert!(mobile_graph_height(5) >= 688);
    }
}

fn node_position(nodes: &[TopologyNode], id: &str, mobile: bool) -> Option<(i32, i32)> {
    nodes
        .iter()
        .position(|node| node.id == id)
        .map(|index| graph_position(index, &nodes[index].node_type, mobile))
}

fn status_class(status: &str) -> &'static str {
    match status {
        "healthy" => "healthy",
        "failed" => "failed",
        _ => "syncing",
    }
}

fn node_scope(node: &TopologyNode) -> String {
    match (node.cluster.is_empty(), node.namespace.is_empty()) {
        (false, false) => format!("{} · {}", node.cluster, node.namespace),
        (false, true) => node.cluster.clone(),
        (true, false) => node.namespace.clone(),
        (true, true) => "Control plane".to_string(),
    }
}

fn truncate(value: &str, max_chars: usize) -> String {
    if value.chars().count() <= max_chars {
        return value.to_string();
    }
    value
        .chars()
        .take(max_chars.saturating_sub(1))
        .collect::<String>()
        + "…"
}
