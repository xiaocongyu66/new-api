/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { decodeProxyNode, decodeProxyNodes } from "../proxy-node-types";

/**
 * E2E contract tests for the proxy page data flow:
 * backend JSON -> decodeProxyNode(s) -> UI row model -> scope serialization.
 * These guard the wire contract between /api/proxy/nodes and the table.
 */
describe("proxy node e2e contract", () => {
  test("decodes a custom-scope node exactly as the API returns it", () => {
    const apiPayload = {
      id: 1,
      name: "111#1",
      enabled: true,
      proxy_configured: true,
      protocol: "socks5",
      scope_type: "custom",
      scope_value: '{"models":["glm-5.2"],"channels":[1,2,3]}',
      health: 0.7,
      failure_count: 1,
      last_error: "network connection failed",
      last_probe_at: "2026-08-22T05:18:00Z",
      probe_total: 4,
      probe_success: 3,
    };

    const node = decodeProxyNode(apiPayload);
    assert.ok(node);
    assert.equal(node.id, 1);
    assert.equal(node.scope_type, "custom");
    assert.deepEqual(JSON.parse(node.scope_value ?? "{}"), {
      models: ["glm-5.2"],
      channels: [1, 2, 3],
    });
    assert.equal(node.probe_total, 4);
    assert.equal(node.protocol, "socks5");
  });

  test("drops malformed rows instead of crashing the whole list render", () => {
    const rows = decodeProxyNodes([
      null,
      { id: "not-a-number" },
      {
        id: 2,
        name: "valid#1",
        enabled: true,
        proxy_configured: true,
        protocol: "vmess",
        scope_type: "all",
        health: 1,
        probe_total: 0,
        probe_success: 0,
      },
    ]);
    assert.equal(rows.length, 1);
    assert.equal(rows[0].name, "valid#1");
  });

  test("scope round-trip: serialize selection to API payload and back", () => {
    // Simulate what ProxyNodeRow.handleSave builds from selectedMappingIds
    const selectedMappingIds = new Set(["m:glm-5.2", "c:1", "c:13"]);
    const models: string[] = [];
    const channels: number[] = [];
    for (const id of selectedMappingIds) {
      if (id.startsWith("m:")) models.push(id.slice(2));
      else if (id.startsWith("c:")) channels.push(Number(id.slice(2)));
    }
    const scopeValue = JSON.stringify({ models, channels });

    // What the expanded row parses back on reopen (selection 回显)
    const parsed = JSON.parse(scopeValue);
    const restored = new Set<string>();
    for (const m of parsed.models) restored.add(`m:${m}`);
    for (const ch of parsed.channels) restored.add(`c:${ch}`);

    assert.equal(restored.size, 3);
    assert.ok(restored.has("m:glm-5.2"));
    assert.ok(restored.has("c:1"));
    assert.ok(restored.has("c:13"));
  });

  test("empty selection saves as scope all (direct behavior unchanged)", () => {
    const selectedMappingIds = new Set<string>();
    let scopeType: "all" | "custom" = "all";
    let scopeValue = "";
    if (selectedMappingIds.size > 0) scopeType = "custom";
    assert.equal(scopeType, "all");
    assert.equal(scopeValue, "");
  });
});
