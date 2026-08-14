//! Resource management tab: list, inspect, scale and delete Karmada resources.

use dioxus::prelude::*;
use serde_json::{Value, json};

use crate::{api, clusters::urlencode};

const KINDS: &[&str] = &[
    "Namespace",
    "Deployment",
    "StatefulSet",
    "DaemonSet",
    "Service",
    "ConfigMap",
    "Secret",
];

#[derive(Clone, PartialEq)]
struct ResourceTarget {
    kind: String,
    namespace: String,
    name: String,
    cluster: String,
}

#[component]
pub fn ResourcesView() -> Element {
    let mut kind = use_signal(|| "Deployment".to_string());
    let mut namespace = use_signal(|| "default".to_string());
    let mut cluster = use_signal(String::new);
    let mut selected = use_signal(|| Option::<ResourceTarget>::None);
    let mut scale_value = use_signal(String::new);
    let mut delete_confirm = use_signal(String::new);
    let mut action_error = use_signal(String::new);
    let mut refresh = use_signal(|| 0u32);

    let clusters =
        use_resource(|| async move { api::request("GET", "/api/karmada/clusters", None).await });
    let resources = use_resource(move || {
        let kind = kind();
        let namespace = namespace();
        let cluster = cluster();
        let _refresh = refresh();
        async move { api::request("GET", &list_path(&kind, &namespace, &cluster), None).await }
    });
    let detail = use_resource(move || async move {
        let target = selected()?;
        let _refresh = refresh();
        Some(api::request("GET", &detail_path(&target), None).await)
    });

    rsx! {
        div { class: "stack",
            section { class: "card stack",
                div { class: "toolbar",
                    label { class: "field",
                        span { "Resource type" }
                        select {
                            value: "{kind()}",
                            onchange: move |event| {
                                kind.set(event.value());
                                selected.set(None);
                            },
                            for option in KINDS {
                                option { value: "{option}", "{option}" }
                            }
                        }
                    }
                    label { class: "field",
                        span { "Cluster" }
                        select {
                            value: "{cluster()}",
                            onchange: move |event| {
                                cluster.set(event.value());
                                selected.set(None);
                            },
                            option { value: "", "All clusters" }
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
                    if is_namespaced(&kind()) {
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
                    }
                    button {
                        class: "button",
                        onclick: move |_| {
                            selected.set(None);
                            refresh.set(refresh() + 1);
                        },
                        "Refresh"
                    }
                }

                match &*resources.read_unchecked() {
                    None => rsx! { p { class: "muted", "Loading resources…" } },
                    Some(Err(message)) => rsx! { p { class: "error", "{message}" } },
                    Some(Ok(data)) => {
                        let items = api::list_field(data, "resources");
                        if items.is_empty() {
                            rsx! { p { class: "muted", "No resource matched the selected filters." } }
                        } else {
                            rsx! { ResourceTable {
                                kind: kind(),
                                cluster: cluster(),
                                resources: items,
                                on_select: move |target: ResourceTarget| {
                                    scale_value.set(String::new());
                                    delete_confirm.set(String::new());
                                    action_error.set(String::new());
                                    selected.set(Some(target));
                                }
                            } }
                        }
                    }
                }
            }

            if let Some(target) = selected() {
                match (detail.pending(), &*detail.read_unchecked()) {
                    (true, _) | (false, None) | (false, Some(None)) => rsx! { p { class: "muted", "Loading resource detail…" } },
                    (false, Some(Some(Err(message)))) => rsx! { p { class: "error", "{message}" } },
                    (false, Some(Some(Ok(data)))) => rsx! { ResourceDetailPanel {
                        target,
                        detail: data.clone(),
                        scale_value,
                        delete_confirm,
                        action_error,
                        refresh,
                        selected,
                    } },
                }
            }
        }
    }
}

#[component]
fn ResourceTable(
    kind: String,
    cluster: String,
    resources: Vec<Value>,
    on_select: EventHandler<ResourceTarget>,
) -> Element {
    rsx! {
        div { class: "table-scroll",
            table { class: "grid",
                thead {
                    tr {
                        th { "Name" }
                        th { "Type" }
                        th { "Cluster" }
                        th { "Namespace" }
                        th { "Status" }
                        th { "Replicas" }
                        th { "Created" }
                    }
                }
                tbody {
                    for resource in resources {
                        ResourceRow {
                            key: "{kind}:{api::text(&resource, \"namespace\")}:{api::text(&resource, \"name\")}:{api::text(&resource, \"cluster\")}",
                            kind: kind.clone(),
                            cluster: cluster.clone(),
                            resource: resource.clone(),
                            on_select,
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn ResourceRow(
    kind: String,
    cluster: String,
    resource: Value,
    on_select: EventHandler<ResourceTarget>,
) -> Element {
    let name = api::text(&resource, "name");
    let namespace = api::text(&resource, "namespace");
    let row_cluster = api::text(&resource, "cluster");
    let display_cluster = if row_cluster.is_empty() {
        if cluster.is_empty() {
            "All clusters".to_string()
        } else {
            cluster.clone()
        }
    } else {
        row_cluster.clone()
    };
    let status = api::text(&resource, "status");
    let status_class = status_class(&status);
    let created = display_or_dash(&api::text(&resource, "createdAt"));
    let namespace_display = display_or_dash(&namespace);
    let replicas = match (
        resource.get("readyReplicas").and_then(Value::as_i64),
        resource.get("replicas").and_then(Value::as_i64),
    ) {
        (Some(ready), Some(total)) => format!("{ready}/{total}"),
        (_, Some(total)) => total.to_string(),
        _ => "—".to_string(),
    };
    rsx! {
        tr { class: "row-clickable",
            onclick: move |_| on_select.call(ResourceTarget {
                kind: kind.clone(),
                namespace: namespace.clone(),
                name: name.clone(),
                cluster: row_cluster.clone(),
            }),
            td { button { class: "link", "{name}" } }
            td { "{kind}" }
            td { "{display_cluster}" }
            td { "{namespace_display}" }
            td { span { class: "{status_class}", "{status}" } }
            td { "{replicas}" }
            td { class: "mono", "{created}" }
        }
    }
}

#[component]
fn ResourceDetailPanel(
    target: ResourceTarget,
    detail: Value,
    mut scale_value: Signal<String>,
    mut delete_confirm: Signal<String>,
    mut action_error: Signal<String>,
    mut refresh: Signal<u32>,
    mut selected: Signal<Option<ResourceTarget>>,
) -> Element {
    let distribution = api::list_field(&detail, "distribution");
    let pods = api::list_field(&detail, "pods");
    let can_write = target.cluster.is_empty();
    let can_scale = target.kind == "Deployment" && can_write;
    let can_delete = can_write;
    let expected_confirm = target.name.clone();
    let namespace = display_or_dash(&target.namespace);
    let detail_cluster = if target.cluster.is_empty() {
        "All clusters".to_string()
    } else {
        target.cluster.clone()
    };
    let scale_target = target.clone();
    let delete_target = target.clone();
    let detail_status = api::text(&detail, "status");

    rsx! {
        section { class: "card stack",
            div {
                h2 { "{target.kind} / {target.name}" }
                p { class: "muted", "Namespace {namespace} · Cluster {detail_cluster} · Status {detail_status}" }
            }
            if !action_error().is_empty() {
                p { class: "error", "{action_error()}" }
            }

            div { class: "summary-grid",
                Count { label: "Replicas", value: optional_i64(&detail, "replicas") }
                Count { label: "Distribution", value: Some(distribution.len() as i64) }
                Count { label: "Pods", value: Some(pods.len() as i64) }
            }

            if !distribution.is_empty() {
                h3 { "Replica distribution" }
                div { class: "table-scroll",
                    table { class: "grid",
                        thead { tr { th { "Cluster" } th { "Replicas" } } }
                        tbody {
                            for item in distribution {
                                tr {
                                    td { "{api::text(&item, \"cluster\")}" }
                                    td { "{api::int(&item, \"replicas\")}" }
                                }
                            }
                        }
                    }
                }
            }

            h3 { "Pods" }
            if pods.is_empty() {
                p { class: "muted", "No pod data available for this workload." }
            } else {
                div { class: "table-scroll",
                    table { class: "grid",
                        thead { tr { th { "Pod" } th { "Cluster" } th { "Phase" } th { "Ready" } th { "Restarts" } } }
                        tbody {
                            for pod in pods {
                                tr {
                                    td { "{api::text(&pod, \"name\")}" }
                                    td { "{api::text(&pod, \"cluster\")}" }
                                    td { "{api::text(&pod, \"phase\")}" }
                                    td { "{api::text(&pod, \"ready\")}" }
                                    td { "{api::int(&pod, \"restarts\")}" }
                                }
                            }
                        }
                    }
                }
            }

            div { class: "detail-actions",
                div { class: "action-card",
                    h3 { "Scale Deployment" }
                    if can_scale {
                        div { class: "confirm-row",
                            input {
                                class: "text-input",
                                value: "{scale_value()}",
                                placeholder: "replicas",
                                oninput: move |event| scale_value.set(event.value())
                            }
                            button {
                                class: "button",
                                onclick: move |_| {
                                    let target = scale_target.clone();
                                    spawn(async move {
                                        action_error.set(String::new());
                                        let replicas = match scale_value().trim().parse::<i64>() {
                                            Ok(value) if (0..=10000).contains(&value) => value,
                                            _ => {
                                                action_error.set("Replicas must be between 0 and 10000.".to_string());
                                                return;
                                            }
                                        };
                                        let path = format!(
                                            "/api/karmada/resources/{}/{}/{}/scale",
                                            urlencode(&target.kind),
                                            urlencode(&target.namespace),
                                            urlencode(&target.name),
                                        );
                                        match api::request("PUT", &path, Some(json!({ "replicas": replicas }))).await {
                                            Ok(_) => refresh.set(refresh() + 1),
                                            Err(message) => action_error.set(message),
                                        }
                                    });
                                },
                                "Scale"
                            }
                        }
                    } else {
                        p { class: "muted", "Select a Deployment from All clusters to scale the Karmada control-plane template." }
                    }
                }

                div { class: "action-card danger-zone",
                    h3 { "Delete resource" }
                    if can_delete {
                        p { class: "muted", "Type the resource name to confirm deletion." }
                        div { class: "confirm-row",
                            input {
                                class: "text-input",
                                value: "{delete_confirm()}",
                                placeholder: "{expected_confirm}",
                                aria_label: "Type {expected_confirm} to confirm deletion",
                                oninput: move |event| delete_confirm.set(event.value())
                            }
                            button {
                                class: "button danger",
                                disabled: delete_confirm() != expected_confirm,
                                aria_label: "Delete {expected_confirm}; enabled after confirmation matches",
                                onclick: move |_| {
                                    let target = delete_target.clone();
                                    spawn(async move {
                                        action_error.set(String::new());
                                        let base_path = detail_path(&target);
                                        let separator = if base_path.contains('?') { "&" } else { "?" };
                                        let path = format!("{base_path}{separator}confirm={}", urlencode(&target.name));
                                        match api::request("DELETE", &path, None).await {
                                            Ok(_) => {
                                                selected.set(None);
                                                refresh.set(refresh() + 1);
                                            }
                                            Err(message) => action_error.set(message),
                                        }
                                    });
                                },
                                "Delete"
                            }
                        }
                    } else {
                        p { class: "muted", "Select All clusters before deleting a Karmada control-plane resource." }
                    }
                }
            }
        }
    }
}

#[component]
fn Count(label: String, value: Option<i64>) -> Element {
    let rendered = value
        .map(|v| v.to_string())
        .unwrap_or_else(|| "—".to_string());
    rsx! {
        div { class: "count",
            div { class: "count-value", "{rendered}" }
            div { class: "muted", "{label}" }
        }
    }
}

fn list_path(kind: &str, namespace: &str, cluster: &str) -> String {
    let mut params = Vec::new();
    if is_namespaced(kind) && !namespace.trim().is_empty() {
        params.push(format!("namespace={}", urlencode(namespace.trim())));
    }
    if !cluster.is_empty() {
        params.push(format!("cluster={}", urlencode(cluster)));
    }
    let mut path = format!("/api/karmada/resources/{}", urlencode(kind));
    if !params.is_empty() {
        path.push('?');
        path.push_str(&params.join("&"));
    }
    path
}

fn detail_path(target: &ResourceTarget) -> String {
    let mut path = if is_namespaced(&target.kind) {
        format!(
            "/api/karmada/resources/{}/{}/{}",
            urlencode(&target.kind),
            urlencode(&target.namespace),
            urlencode(&target.name)
        )
    } else {
        format!(
            "/api/karmada/resources/{}/{}",
            urlencode(&target.kind),
            urlencode(&target.name)
        )
    };
    if !target.cluster.is_empty() {
        path.push_str("?cluster=");
        path.push_str(&urlencode(&target.cluster));
    }
    path
}

fn is_namespaced(kind: &str) -> bool {
    kind != "Namespace"
}

fn status_class(status: &str) -> &'static str {
    match status {
        "Ready" | "Running" | "Active" => "pill ready",
        "—" | "" => "pill unknown",
        _ => "pill not-ready",
    }
}

fn display_or_dash(value: &str) -> String {
    if value.is_empty() {
        "—".to_string()
    } else {
        value.to_string()
    }
}

fn optional_i64(value: &Value, key: &str) -> Option<i64> {
    value.get(key).and_then(Value::as_i64)
}
