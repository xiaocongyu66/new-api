//! Policy management tab for Karmada propagation and override policies.

use dioxus::prelude::*;
use serde_json::{Value, json};

use crate::{api, clusters::urlencode};

const POLICY_TYPES: &[&str] = &[
    "PropagationPolicy",
    "ClusterPropagationPolicy",
    "OverridePolicy",
    "ClusterOverridePolicy",
];

#[derive(Clone, PartialEq)]
struct PolicyTarget {
    kind: String,
    namespace: String,
    name: String,
}

#[component]
pub fn PoliciesView() -> Element {
    let mut kind = use_signal(|| "PropagationPolicy".to_string());
    let mut namespace = use_signal(|| "default".to_string());
    let mut selected = use_signal(|| Option::<PolicyTarget>::None);
    let mut show_create = use_signal(|| false);
    let mut refresh = use_signal(|| 0u32);
    let mut action_message = use_signal(String::new);

    let policies = use_resource(move || {
        let kind = kind();
        let namespace = namespace();
        let _refresh = refresh();
        async move { api::request("GET", &list_path(&kind, &namespace), None).await }
    });
    let detail = use_resource(move || async move {
        let target = selected()?;
        let _refresh = refresh();
        Some(api::request("GET", &policy_path(&target), None).await)
    });

    rsx! {
        div { class: "stack policy-shell",
            section { class: "card stack",
                div { class: "toolbar policy-toolbar",
                    label { class: "field policy-type-field",
                        span { "Policy type" }
                        select {
                            value: "{kind()}",
                            onchange: move |event| {
                                kind.set(event.value());
                                selected.set(None);
                                show_create.set(false);
                            },
                            for option in POLICY_TYPES {
                                option { value: "{option}", "{option}" }
                            }
                        }
                    }
                    if is_namespaced(&kind()) {
                        label { class: "field namespace-field",
                            span { "Namespace" }
                            input {
                                class: "text-input",
                                value: "{namespace()}",
                                placeholder: "default",
                                oninput: move |event| {
                                    namespace.set(event.value());
                                    selected.set(None);
                                    show_create.set(false);
                                }
                            }
                        }
                    }
                    div { class: "toolbar-actions",
                        button {
                            class: "button secondary",
                            onclick: move |_| {
                                selected.set(None);
                                refresh.set(refresh() + 1);
                            },
                            "Refresh"
                        }
                        button {
                            class: "button",
                            onclick: move |_| {
                                action_message.set(String::new());
                                selected.set(None);
                                show_create.set(!show_create());
                            },
                            if show_create() { "Close creator" } else { "Create policy" }
                        }
                    }
                }

                match &*policies.read_unchecked() {
                    None => rsx! { p { class: "muted", "Loading policies…" } },
                    Some(Err(message)) => rsx! { p { class: "error", "{message}" } },
                    Some(Ok(data)) => {
                        let items = api::list_field(data, "items");
                        if items.is_empty() {
                            rsx! { div { class: "empty-panel", p { "No policies match these filters." } } }
                        } else {
                            rsx! { PolicyTable {
                                policies: items,
                                on_select: move |target| {
                                    action_message.set(String::new());
                                    selected.set(Some(target));
                                    show_create.set(false);
                                }
                            } }
                        }
                    }
                }
            }

            if show_create() {
                PolicyEditor {
                    kind: kind(),
                    namespace: namespace(),
                    on_saved: move |_| {
                        show_create.set(false);
                        action_message.set("Policy created.".to_string());
                        refresh.set(refresh() + 1);
                    }
                }
            }

            if !action_message().is_empty() {
                p { class: "notice", "{action_message()}" }
            }

            if let Some(target) = selected() {
                match (detail.pending(), &*detail.read_unchecked()) {
                    (true, _) | (false, None) | (false, Some(None)) => rsx! { p { class: "muted", "Loading policy detail…" } },
                    (false, Some(Some(Err(message)))) => rsx! { p { class: "error", "{message}" } },
                    (false, Some(Some(Ok(data)))) => rsx! { PolicyDetail {
                        target,
                        detail: data.clone(),
                        selected,
                        refresh,
                        action_message,
                    } },
                }
            }
        }
    }
}

#[component]
fn PolicyTable(policies: Vec<Value>, on_select: EventHandler<PolicyTarget>) -> Element {
    rsx! {
        div { class: "table-scroll",
            table { class: "grid policy-table",
                thead { tr { th { "Name" } th { "Type" } th { "Namespace" } th { "Created" } th { "" } } }
                tbody {
                    for policy in policies {
                        PolicyRow { policy, on_select }
                    }
                }
            }
        }
    }
}

#[component]
fn PolicyRow(policy: Value, on_select: EventHandler<PolicyTarget>) -> Element {
    let name = api::text(&policy, "name");
    let kind = api::text(&policy, "type");
    let namespace = api::text(&policy, "namespace");
    let created = api::text(&policy, "createdAt");
    let target = PolicyTarget {
        kind: kind.clone(),
        namespace: namespace.clone(),
        name: name.clone(),
    };
    rsx! {
        tr { class: "row-clickable",
            onclick: move |_| on_select.call(target.clone()),
            td { strong { "{name}" } }
            td { span { class: "policy-kind", "{kind}" } }
            td { if namespace.is_empty() { "Cluster scoped" } else { "{namespace}" } }
            td { class: "mono", if created.is_empty() { "—" } else { "{created}" } }
            td { button { class: "icon-button", aria_label: "Open {name}", "→" } }
        }
    }
}

#[component]
fn PolicyEditor(kind: String, namespace: String, on_saved: EventHandler<()>) -> Element {
    let mut mode = use_signal(|| "structured".to_string());
    let mut name = use_signal(String::new);
    let mut resource_kind = use_signal(|| "Deployment".to_string());
    let mut resource_api_version = use_signal(|| "apps/v1".to_string());
    let mut resource_name = use_signal(String::new);
    let mut clusters = use_signal(String::new);
    let mut overriders = use_signal(|| "[]".to_string());
    let mut yaml_body = use_signal(|| default_yaml(&kind, &namespace));
    let mut error = use_signal(String::new);
    let namespaced = is_namespaced(&kind);
    let is_override = kind.contains("Override");

    rsx! {
        section { class: "card stack policy-editor",
            div { class: "section-heading",
                div { h2 { "Create {kind}" } p { class: "muted", "Choose guided fields or paste one validated YAML document." } }
                div { class: "mode-switch", role: "tablist", aria_label: "Policy editor mode",
                    button { class: if mode() == "structured" { "mode active" } else { "mode" }, onclick: move |_| mode.set("structured".to_string()), "Structured" }
                    button { class: if mode() == "yaml" { "mode active" } else { "mode" }, onclick: move |_| mode.set("yaml".to_string()), "YAML" }
                }
            }
            if !error().is_empty() { p { class: "error", "{error()}" } }
            if mode() == "structured" {
                div { class: "form-grid",
                    label { class: "field", span { "Policy name" } input { class: "text-input", value: "{name()}", oninput: move |e| name.set(e.value()), placeholder: "deploy-to-edge" } }
                    if namespaced { label { class: "field", span { "Namespace" } input { class: "text-input", value: "{namespace}", disabled: true } } }
                    label { class: "field", span { "Matched resource API version" } input { class: "text-input", value: "{resource_api_version()}", oninput: move |e| resource_api_version.set(e.value()), placeholder: "apps/v1" } }
                    label { class: "field", span { "Matched resource kind" } input { class: "text-input", value: "{resource_kind()}", oninput: move |e| resource_kind.set(e.value()) } }
                    label { class: "field", span { "Matched resource name" } input { class: "text-input", value: "{resource_name()}", oninput: move |e| resource_name.set(e.value()), placeholder: "web" } }
                    if !is_override { label { class: "field span-two", span { "Target clusters (comma separated)" } input { class: "text-input", value: "{clusters()}", oninput: move |e| clusters.set(e.value()), placeholder: "member-a, member-b" } } }
                    if is_override { label { class: "field span-two", span { "Overriders JSON" } textarea { class: "yaml-editor compact", value: "{overriders()}", oninput: move |e| overriders.set(e.value()) } } }
                }
            } else {
                label { class: "field", span { "Policy YAML" } textarea { class: "yaml-editor", spellcheck: "false", value: "{yaml_body()}", oninput: move |e| yaml_body.set(e.value()) } }
            }
            div { class: "editor-footer",
                span { class: "muted", "The API validates one YAML object and a four-kind allowlist before forwarding." }
                button {
                    class: "button",
                    onclick: move |_| {
                        let kind = kind.clone();
                        let namespace = namespace.clone();
                        spawn(async move {
                            error.set(String::new());
                            let body = if mode() == "yaml" {
                                json!({ "yaml": yaml_body() })
                            } else if name().trim().is_empty() {
                                error.set("Policy name is required.".to_string());
                                return;
                            } else {
                                let selector = json!({ "apiVersion": resource_api_version(), "kind": resource_kind(), "name": resource_name() });
                                let spec = if kind.contains("Override") {
                                    match serde_json::from_str::<Value>(&overriders()) {
                                        Ok(items) => json!({ "resourceSelectors": [selector], "overrideRules": [{ "targetCluster": {}, "overriders": items }] }),
                                        Err(parse_error) => { error.set(format!("Invalid overriders JSON: {parse_error}")); return; }
                                    }
                                } else {
                                    let cluster_names: Vec<String> = clusters().split(',').map(str::trim).filter(|value| !value.is_empty()).map(str::to_string).collect();
                                    json!({ "resourceSelectors": [selector], "placement": { "clusterAffinity": { "clusterNames": cluster_names } } })
                                };
                                let namespaced = is_namespaced(&kind);
                                let mut body = json!({ "type": kind, "name": name(), "spec": spec });
                                if namespaced {
                                    body["namespace"] = json!(namespace);
                                }
                                body
                            };
                            match api::request("POST", "/api/karmada/policies", Some(body)).await {
                                Ok(_) => on_saved.call(()),
                                Err(message) => error.set(message),
                            }
                        });
                    },
                    "Create policy"
                }
            }
        }
    }
}

#[component]
fn PolicyDetail(
    target: PolicyTarget,
    detail: Value,
    mut selected: Signal<Option<PolicyTarget>>,
    mut refresh: Signal<u32>,
    mut action_message: Signal<String>,
) -> Element {
    let yaml = api::text(&detail, "yaml");
    let edit_yaml = yaml.clone();
    let matches = api::list_field(&detail, "matched_resources");
    let mut editing = use_signal(|| false);
    let mut editor = use_signal(|| yaml.clone());
    let mut delete_confirm = use_signal(String::new);
    let mut show_delete_confirm = use_signal(|| false);
    let mut error = use_signal(String::new);
    let update_target = target.clone();
    let delete_target = target.clone();

    rsx! {
        div { class: "policy-drawer-layer",
            button {
                class: "drawer-scrim",
                aria_label: "Close policy details",
                onclick: move |_| selected.set(None)
            }
            aside {
                class: "policy-drawer stack",
                role: "dialog",
                aria_modal: "true",
                aria_label: "Policy details",
                div { class: "section-heading",
                    div {
                        p { class: "eyebrow", "{target.kind}" }
                        h2 { "{target.name}" }
                        p { class: "muted",
                            if target.namespace.is_empty() { "Cluster scoped" } else { "Namespace {target.namespace}" }
                        }
                    }
                    div { class: "drawer-actions",
                        button {
                            class: "button secondary",
                            onclick: move |_| {
                                if editing() {
                                    editing.set(false);
                                } else {
                                    editor.set(edit_yaml.clone());
                                    editing.set(true);
                                }
                            },
                            if editing() { "Cancel edit" } else { "Edit YAML" }
                        }
                        button {
                            class: "icon-button drawer-close",
                            aria_label: "Close policy details",
                            onclick: move |_| selected.set(None),
                            "×"
                        }
                    }
                }
                if !error().is_empty() {
                    p { class: "error", "{error()}" }
                }
                if editing() {
                    textarea {
                        class: "yaml-editor",
                        spellcheck: "false",
                        value: "{editor()}",
                        oninput: move |event| editor.set(event.value())
                    }
                    button {
                        class: "button save-policy",
                        onclick: move |_| {
                            let target = update_target.clone();
                            spawn(async move {
                                error.set(String::new());
                                match api::request("PUT", &policy_path(&target), Some(json!({ "yaml": editor() }))).await {
                                    Ok(_) => {
                                        editing.set(false);
                                        action_message.set("Policy updated.".to_string());
                                        refresh.set(refresh() + 1);
                                    }
                                    Err(message) => error.set(message),
                                }
                            });
                        },
                        "Save policy"
                    }
                } else {
                    div { class: "yaml-block", aria_label: "Policy YAML with syntax highlighting",
                        code {
                            for (index, line) in yaml.lines().enumerate() {
                                div { class: "yaml-line",
                                    span { class: "line-number", "{index + 1}" }
                                    span { class: "{yaml_class(line)}", "{line}" }
                                }
                            }
                        }
                    }
                }

                div { class: "matched-section",
                    div { class: "section-heading compact",
                        h3 { "Matched resources" }
                        span { class: "count-badge", "{matches.len()} live" }
                    }
                    if matches.is_empty() {
                        p { class: "muted", "No ResourceBinding currently matches this policy." }
                    } else {
                        div { class: "table-scroll",
                            table { class: "grid",
                                thead { tr {
                                    th { "Resource" }
                                    th { "Kind" }
                                    th { "Namespace" }
                                    th { "API version" }
                                } }
                                tbody {
                                    for item in matches {
                                        MatchedResourceRow { item }
                                    }
                                }
                            }
                        }
                    }
                }

                div { class: "danger-zone policy-danger",
                    div {
                        h3 { "Delete policy" }
                        p { class: "muted", "Deletion requires an explicit name confirmation." }
                    }
                    button {
                        class: "button danger",
                        onclick: move |_| {
                            error.set(String::new());
                            delete_confirm.set(String::new());
                            show_delete_confirm.set(true);
                        },
                        "Delete policy"
                    }
                }
            }

            if show_delete_confirm() {
                div { class: "policy-confirm-layer",
                    div {
                        class: "policy-confirm-dialog",
                        role: "alertdialog",
                        aria_modal: "true",
                        aria_label: "Confirm policy deletion",
                        h3 { "Delete {target.name}?" }
                        p { class: "muted", "Type the policy name to permanently delete it." }
                        input {
                            class: "text-input",
                            value: "{delete_confirm()}",
                            placeholder: "{target.name}",
                            oninput: move |event| delete_confirm.set(event.value()),
                            aria_label: "Type {target.name} to confirm deletion"
                        }
                        if !error().is_empty() {
                            p { class: "error", "{error()}" }
                        }
                        div { class: "confirm-row policy-confirm-actions",
                            button {
                                class: "button secondary",
                                onclick: move |_| show_delete_confirm.set(false),
                                "Cancel"
                            }
                            button {
                                class: "button danger",
                                disabled: delete_confirm() != target.name,
                                onclick: move |_| {
                                    let target = delete_target.clone();
                                    spawn(async move {
                                        error.set(String::new());
                                        let path = format!("{}?confirm={}", policy_path(&target), urlencode(&target.name));
                                        match api::request("DELETE", &path, None).await {
                                            Ok(_) => {
                                                show_delete_confirm.set(false);
                                                selected.set(None);
                                                action_message.set("Policy deleted.".to_string());
                                                refresh.set(refresh() + 1);
                                            }
                                            Err(message) => error.set(message),
                                        }
                                    });
                                },
                                "Delete permanently"
                            }
                        }
                    }
                }
            }
        }
    }
}

#[component]
fn MatchedResourceRow(item: Value) -> Element {
    let name = api::text(&item, "name");
    let kind = api::text(&item, "kind");
    let namespace = api::text(&item, "namespace");
    let api_version = api::text(&item, "apiVersion");
    rsx! {
        tr {
            td { strong { "{name}" } }
            td { "{kind}" }
            td { if namespace.is_empty() { "—" } else { "{namespace}" } }
            td { class: "mono", "{api_version}" }
        }
    }
}

fn list_path(kind: &str, namespace: &str) -> String {
    let mut path = format!("/api/karmada/policies?type={}", urlencode(kind));
    if is_namespaced(kind) && !namespace.trim().is_empty() {
        path.push_str("&namespace=");
        path.push_str(&urlencode(namespace.trim()));
    }
    path
}

fn policy_path(target: &PolicyTarget) -> String {
    if is_namespaced(&target.kind) {
        format!(
            "/api/karmada/policies/{}/namespaces/{}/{}",
            urlencode(&target.kind),
            urlencode(&target.namespace),
            urlencode(&target.name)
        )
    } else {
        format!(
            "/api/karmada/policies/{}/{}",
            urlencode(&target.kind),
            urlencode(&target.name)
        )
    }
}

fn is_namespaced(kind: &str) -> bool {
    matches!(kind, "PropagationPolicy" | "OverridePolicy")
}

fn default_yaml(kind: &str, namespace: &str) -> String {
    let namespace_line = if is_namespaced(kind) {
        format!("  namespace: {namespace}\n")
    } else {
        String::new()
    };
    format!(
        "apiVersion: policy.karmada.io/v1alpha1\nkind: {kind}\nmetadata:\n  name: new-policy\n{namespace_line}spec:\n  resourceSelectors:\n    - apiVersion: apps/v1\n      kind: Deployment\n      name: web\n"
    )
}

fn yaml_class(line: &str) -> &'static str {
    let trimmed = line.trim_start();
    if trimmed.starts_with('#') {
        "yaml-comment"
    } else if trimmed.starts_with('-') {
        "yaml-list"
    } else if trimmed.contains(':') {
        "yaml-key"
    } else {
        "yaml-value"
    }
}
