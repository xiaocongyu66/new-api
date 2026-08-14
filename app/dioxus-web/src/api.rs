//! Admin API access for the Karmada panel.
//!
//! The panel is served from the same origin as the new-api backend, so the
//! browser sends the existing admin session cookie automatically. Requests go
//! through `document::eval` because the panel intentionally carries no HTTP
//! client crate: the browser's own `fetch` is the platform feature for this.

use dioxus::prelude::*;
use serde_json::{Value, json};

/// Error text shown to the operator when a request cannot be completed.
pub type ApiError = String;

/// Performs a JSON request against the admin API and returns the `data` payload
/// of the `{success, message, data}` envelope used by every new-api endpoint.
pub async fn request(method: &str, path: &str, body: Option<Value>) -> Result<Value, ApiError> {
    // The script is a fixed template; the request is described by JSON values
    // injected through `dioxus.send`, never by string interpolation, so a
    // resource name can never break out of the surrounding JavaScript.
    let eval = document::eval(
        r#"
        const req = await dioxus.recv();
        const init = { method: req.method, headers: { 'Accept': 'application/json' }, credentials: 'same-origin' };
        if (req.body !== null) {
            init.headers['Content-Type'] = 'application/json';
            init.body = JSON.stringify(req.body);
        }
        let response;
        try {
            response = await fetch(req.path, init);
        } catch (err) {
            return { ok: false, message: String(err) };
        }
        const text = await response.text();
        let parsed = null;
        try { parsed = text ? JSON.parse(text) : null; } catch (err) { parsed = null; }
        if (!response.ok) {
            const message = (parsed && parsed.message) ? parsed.message : ('HTTP ' + response.status);
            return { ok: false, message: message };
        }
        if (parsed && parsed.success === false) {
            return { ok: false, message: parsed.message || 'request failed' };
        }
        return { ok: true, data: parsed ? parsed.data : null };
        "#,
    );
    eval.send(json!({
        "method": method,
        "path": path,
        "body": body,
    }))
    .map_err(|err| err.to_string())?;
    let result: Value = eval.join().await.map_err(|err| err.to_string())?;
    if result.get("ok").and_then(Value::as_bool) == Some(true) {
        return Ok(result.get("data").cloned().unwrap_or(Value::Null));
    }
    Err(result
        .get("message")
        .and_then(Value::as_str)
        .unwrap_or("request failed")
        .to_string())
}

/// Reads a JSON list from `data.<key>`, tolerating a null payload.
pub fn list_field(data: &Value, key: &str) -> Vec<Value> {
    data.get(key)
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
}

/// Reads a string field, returning "" when absent or of another type.
pub fn text(value: &Value, key: &str) -> String {
    value
        .get(key)
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string()
}

/// Reads an integer field, returning 0 when absent.
pub fn int(value: &Value, key: &str) -> i64 {
    value.get(key).and_then(Value::as_i64).unwrap_or_default()
}

/// Formats an optional numeric metric. A missing metric renders as an em dash
/// rather than a zero, so "no data" is visually distinct from "idle".
pub fn metric(value: &Value, key: &str, suffix: &str) -> String {
    match value.get(key).and_then(Value::as_f64) {
        Some(number) => format!("{number:.1}{suffix}"),
        None => "—".to_string(),
    }
}
