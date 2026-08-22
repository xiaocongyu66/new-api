import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  CheckCircle2,
  Loader2,
  Play,
  Trash2,
  XCircle,
  Zap,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { RouteModelPicker } from "./components/route-model-picker";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { toast } from "@/hooks/use-toast";
import { cn } from "@/lib/utils";

import {
  clearProxyNodeErrors,
  deleteProxyNode,
  fetchProxyNodeDetail,
  fetchProxyNodeReport,
  fetchProxyNodes,
  setProxyNodesEnabled,
  testProxyNode,
  updateProxyNode,
} from "./proxy-node-api";
import { ProxyNodeQuickAdd } from "./proxy-node-quick-add";
import { type ProxyNodeScopeType } from "./proxy-node-scope";
import type { ProxyNode } from "./proxy-node-types";
export function ProxyNodeView() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<ProxyNode | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(() => new Set());
  const [sortConfig, setSortConfig] = useState<{
    key: "name" | "protocol" | "health" | "success" | "failure" | "probe_count";
    order: "asc" | "desc";
  }>({
    key: "name",
    order: "asc",
  });
  const nodesQuery = useQuery({
    queryKey: ["proxy-nodes"],
    queryFn: fetchProxyNodes,
    retry: false,
  });
  const reportQuery = useQuery({
    queryKey: ["proxy-nodes", "report"],
    queryFn: fetchProxyNodeReport,
    retry: false,
  });
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["proxy-nodes"] });
  };

  const deleteMutation = useMutation({
    mutationFn: deleteProxyNode,
    onSuccess: () => {
      setDeleteTarget(null);
      invalidate();
    },
    onError: (error: Error) =>
      toast.error(error.message || t("Failed to delete")),
  });
  const testMutation = useMutation({
    mutationFn: testProxyNode,
    onSuccess: invalidate,
    onError: (error: Error) => toast.error(error.message || t("Test failed")),
  });
  const enabledMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      setProxyNodesEnabled([...selectedIds], enabled),
    onSuccess: () => {
      setSelectedIds(new Set());
      invalidate();
    },
    onError: (error: Error) =>
      toast.error(error.message || t("Batch update failed")),
  });
  const clearErrorsMutation = useMutation({
    mutationFn: () => clearProxyNodeErrors([...selectedIds]),
    onSuccess: () => {
      setSelectedIds(new Set());
      invalidate();
    },
    onError: (error: Error) =>
      toast.error(error.message || t("Failed to clear errors")),
  });

  const nodes = useMemo(() => {
    const items = [...(nodesQuery.data ?? [])];
    const { key, order } = sortConfig;
    return items.sort((left, right) => {
      let comparison = 0;
      if (key === "name") {
        comparison = left.name.localeCompare(right.name);
      } else if (key === "protocol") {
        comparison = (left.protocol || "SOCKS5").localeCompare(right.protocol || "SOCKS5");
      } else if (key === "health") {
        comparison = left.health - right.health;
      } else if (key === "success") {
        const leftRate = left.probe_total ? left.probe_success / left.probe_total : 0;
        const rightRate = right.probe_total ? right.probe_success / right.probe_total : 0;
        comparison = leftRate - rightRate;
      } else if (key === "failure") {
        const leftRate = left.probe_total ? (left.probe_total - left.probe_success) / left.probe_total : 0;
        const rightRate = right.probe_total ? (right.probe_total - right.probe_success) / right.probe_total : 0;
        comparison = leftRate - rightRate;
      } else if (key === "probe_count") {
        comparison = left.probe_total - right.probe_total;
      }
      return order === "asc" ? comparison : -comparison;
    });
  }, [nodesQuery.data, sortConfig]);

  const handleSort = (
    key: "name" | "protocol" | "health" | "success" | "failure" | "probe_count"
  ) => {
    setSortConfig((prev) => ({
      key,
      order: prev.key === key && prev.order === "asc" ? "desc" : "asc",
    }));
  };
  const report = reportQuery.data;
  const selectedCount = selectedIds.size;

  return (
    <div className="space-y-4">
      {selectedCount > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border p-2">
          <span className="text-muted-foreground text-sm">
            {t("{{count}} selected", { count: selectedCount })}
          </span>
          <Button
            size="sm"
            variant="outline"
            onClick={() => enabledMutation.mutate(true)}
            disabled={enabledMutation.isPending}
          >
            {t("Enable Selected")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => enabledMutation.mutate(false)}
            disabled={enabledMutation.isPending}
          >
            {t("Disable Selected")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => clearErrorsMutation.mutate()}
            disabled={clearErrorsMutation.isPending}
          >
            {t("Clear Errors")}
          </Button>
        </div>
      )}

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <NodeMetric
          icon={<Activity className="size-4 text-sky-500" />}
          label={t("Nodes")}
          value={`${report?.enabled ?? 0} / ${report?.total ?? 0}`}
          detail={t("Proxy {{enabled}} · Healthy {{healthy}}", {
            enabled: report?.enabled ?? 0,
            healthy: report?.healthy ?? 0,
          })}
        />
        <NodeMetric
          icon={<CheckCircle2 className="size-4 text-emerald-500" />}
          label={t("Success Rate")}
          value={
            report?.probe_total ? `${report.success_rate.toFixed(1)}%` : "-"
          }
          detail={t("Success {{success}} / Total {{total}}", {
            success: report?.probe_success ?? 0,
            total: report?.probe_total ?? 0,
          })}
        />
        <NodeMetric
          icon={<XCircle className="size-4 text-rose-500" />}
          label={t("Failure Rate")}
          value={
            report?.probe_total ? `${report.failure_rate.toFixed(1)}%` : "-"
          }
          detail={t("Failed {{failed}} / Total {{total}}", {
            failed: report?.probe_failed ?? 0,
            total: report?.probe_total ?? 0,
          })}
        />
        <NodeMetric
          icon={<Zap className="size-4 text-amber-500" />}
          label={t("Total Requests")}
          value={String(report?.probe_total ?? 0)}
          detail={t("In-memory stats, reset on restart")}
        />
      </div>

      <ProxyNodeQuickAdd onAdded={invalidate} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("Proxy Nodes")}</CardTitle>
        </CardHeader>
        <CardContent>
          {nodesQuery.isLoading && (
            <p className="text-muted-foreground text-sm">{t("Loading...")}</p>
          )}
          {nodesQuery.error && (
            <p className="text-destructive text-sm">
              {(nodesQuery.error as Error).message}
            </p>
          )}
          {!nodesQuery.isLoading && !nodesQuery.error && nodes.length === 0 && (
            <p className="text-muted-foreground text-sm">
              {t(
                "No proxy nodes are configured. Existing global proxy and direct channel behavior are unchanged.",
              )}
            </p>
          )}
          {nodes.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-muted-foreground/80 border-b text-xs font-medium text-left bg-muted/20">
                  <tr>
                    <th className="p-2.5 w-10">
                      <span className="sr-only">{t("Select")}</span>
                    </th>
                    <th className="p-2.5 max-w-[220px]">
                      <SortHeaderButton
                        label={t("Name")}
                        sortKey="name"
                        activeKey={sortConfig.key}
                        order={sortConfig.order}
                        onClick={() => handleSort("name")}
                      />
                    </th>
                    <th className="p-2.5 w-28">
                      <SortHeaderButton
                        label={t("Protocol")}
                        sortKey="protocol"
                        activeKey={sortConfig.key}
                        order={sortConfig.order}
                        onClick={() => handleSort("protocol")}
                      />
                    </th>
                    <th className="p-2.5 w-24">
                      <SortHeaderButton
                        label={t("Health")}
                        sortKey="health"
                        activeKey={sortConfig.key}
                        order={sortConfig.order}
                        onClick={() => handleSort("health")}
                      />
                    </th>
                    <th className="p-2.5 w-28">
                      <SortHeaderButton
                        label={t("Success Rate")}
                        sortKey="success"
                        activeKey={sortConfig.key}
                        order={sortConfig.order}
                        onClick={() => handleSort("success")}
                      />
                    </th>
                    <th className="p-2.5 w-28">
                      <SortHeaderButton
                        label={t("Failure Rate")}
                        sortKey="failure"
                        activeKey={sortConfig.key}
                        order={sortConfig.order}
                        onClick={() => handleSort("failure")}
                      />
                    </th>
                    <th className="p-2.5 w-24">
                      <SortHeaderButton
                        label={t("Requests")}
                        sortKey="probe_count"
                        activeKey={sortConfig.key}
                        order={sortConfig.order}
                        onClick={() => handleSort("probe_count")}
                      />
                    </th>
                    <th className="p-2.5 w-20 text-right">
                      <span className="sr-only">{t("Actions")}</span>
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/40">
                  {nodes.map((node) => (
                    <ProxyNodeRow
                      key={node.id}
                      node={node}
                      selected={selectedIds.has(node.id)}
                      onSelectedChange={(selected) => {
                        setSelectedIds((current) => {
                          const next = new Set(current);
                          if (selected) next.add(node.id);
                          else next.delete(node.id);
                          return next;
                        });
                      }}
                      onTest={() => testMutation.mutate(node.id)}
                      testing={
                        testMutation.isPending &&
                        testMutation.variables === node.id
                      }
                      onDelete={() => setDeleteTarget(node)}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("Delete Proxy Node")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('Delete proxy node "{{name}}"? This cannot be undone.', {
                name: deleteTarget?.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("Cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (deleteTarget) deleteMutation.mutate(deleteTarget.id);
              }}
            >
              {t("Delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function NodeMetric(props: {
  icon?: React.ReactNode;
  label: string;
  value: string;
  detail: string;
}) {
  return (
    <Card className="relative overflow-hidden bg-card/60 backdrop-blur-xs">
      <CardHeader className="flex flex-row items-center justify-between pb-1.5 pt-3.5 px-4 space-y-0">
        <CardTitle className="text-xs font-medium text-muted-foreground">
          {props.label}
        </CardTitle>
        {props.icon}
      </CardHeader>
      <CardContent className="px-4 pb-3.5 pt-0">
        <p className="text-xl sm:text-2xl font-bold tracking-tight text-foreground">
          {props.value}
        </p>
        <p className="text-muted-foreground text-xs mt-1 truncate">
          {props.detail}
        </p>
      </CardContent>
    </Card>
  );
}

function SortHeaderButton(props: {
  label: string;
  sortKey: string;
  activeKey: string;
  order: "asc" | "desc";
  onClick: () => void;
}) {
  const isActive = props.sortKey === props.activeKey;
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1 font-medium transition-colors cursor-pointer select-none",
        isActive ? "text-foreground font-semibold" : "text-muted-foreground hover:text-foreground"
      )}
      onClick={props.onClick}
    >
      <span>{props.label}</span>
      {isActive ? (
        props.order === "asc" ? (
          <ArrowUp className="size-3 text-primary stroke-[2.5]" />
        ) : (
          <ArrowDown className="size-3 text-primary stroke-[2.5]" />
        )
      ) : (
        <ArrowUpDown className="size-3 text-muted-foreground/40" />
      )}
    </button>
  );
}

function ProxyNodeRow(props: {
  node: ProxyNode;
  selected: boolean;
  testing: boolean;
  onSelectedChange: (selected: boolean) => void;
  onTest: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const node = props.node;

  // Local state for editing inside expanded accordion
  const [proxyLink, setProxyLink] = useState("");
  const [selectedMappingIds, setSelectedMappingIds] = useState<Set<string>>(() => new Set());
  const [loadedDetail, setLoadedDetail] = useState(false);

  // Fetch full proxy detail once when expanded
  useEffect(() => {
    if (expanded && !loadedDetail) {
      void fetchProxyNodeDetail(node.id).then((detail) => {
        setProxyLink(detail.proxy);
        setLoadedDetail(true);
      });
    }
  }, [expanded, loadedDetail, node.id]);

  // Parse node scope to build real active items and selected IDs
  useEffect(() => {
    const selSet = new Set<string>();
    if (node.scope_type === "custom" && node.scope_value) {
      try {
        const parsed = JSON.parse(node.scope_value);
        if (parsed && typeof parsed === "object") {
          const models: string[] = Array.isArray(parsed.models) ? parsed.models : [];
          const channels: (number | string)[] = Array.isArray(parsed.channels) ? parsed.channels : [];
          for (const m of models) selSet.add(`m:${m}`);
          for (const ch of channels) selSet.add(`c:${ch}`);
        }
      } catch {
        // Ignore JSON error
      }
    } else if (node.scope_type === "channel") {
      selSet.add(`c:${node.scope_value}`);
    }
    setSelectedMappingIds(selSet);
  }, [node]);

  const handleToggleMapping = (id: string) => {
    setSelectedMappingIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const updateMutation = useMutation({
    mutationFn: (payload: { proxy: string; scopeType: ProxyNodeScopeType; scopeValue: string }) => {
      return updateProxyNode(node.id, {
        name: node.name,
        enabled: node.enabled,
        proxy: payload.proxy,
        scope_type: payload.scopeType,
        scope_value: payload.scopeValue,
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["proxy-nodes"] });
      toast.success(t("Saved successfully"));
    },
    onError: (err: Error) => {
      toast.error(err.message || t("Failed to save"));
    },
  });

  const handleSave = () => {
    let scopeType: ProxyNodeScopeType = "all";
    let scopeValue = "";
    if (selectedMappingIds.size > 0) {
      const models: string[] = [];
      const channels: number[] = [];
      for (const id of selectedMappingIds) {
        if (id.startsWith("m:")) models.push(id.slice(2));
        else if (id.startsWith("c:")) channels.push(Number(id.slice(2)));
      }
      scopeType = "custom";
      scopeValue = JSON.stringify({ models, channels });
    }
    updateMutation.mutate({
      proxy: proxyLink,
      scopeType,
      scopeValue,
    });
  };

  const successRate = node.probe_total
    ? `${((node.probe_success / node.probe_total) * 100).toFixed(0)}%`
    : "-";
  const failureRate = node.probe_total
    ? `${(((node.probe_total - node.probe_success) / node.probe_total) * 100).toFixed(0)}%`
    : "-";

  return (
    <>
      <tr
        onClick={() => setExpanded((prev) => !prev)}
        className={cn(
          "cursor-pointer transition-colors hover:bg-muted/30 group",
          !node.enabled && "opacity-50",
          expanded && "bg-muted/20"
        )}
      >
        <td
          className="p-2.5 align-middle w-10"
          onClick={(e) => e.stopPropagation()}
        >
          <Checkbox
            checked={props.selected}
            aria-label={t("Select {{name}}", { name: node.name })}
            onCheckedChange={(checked) =>
              props.onSelectedChange(checked === true)
            }
          />
        </td>
        <td className="p-2.5 font-medium text-foreground align-middle max-w-[220px]">
          <span className="block truncate" title={node.name}>{node.name}</span>
        </td>
        <td className="p-2.5 align-middle w-28">
          <span className="inline-flex items-center rounded-md bg-muted/60 px-2 py-0.5 text-[11px] font-semibold tracking-wide uppercase text-foreground/80">
            {node.protocol || "SOCKS5"}
          </span>
        </td>
        <td className="p-2.5 align-middle font-medium text-muted-foreground w-24">
          {`${(node.health * 100).toFixed(0)}%`}
        </td>
        <td className="p-2.5 align-middle font-medium w-28">
          <span className={node.probe_total ? "text-emerald-500 font-semibold" : "text-emerald-500/50"}>
            {successRate === "-" ? "—" : successRate}
          </span>
        </td>
        <td className="p-2.5 align-middle font-medium w-28">
          <span className={node.probe_total ? "text-rose-500 font-semibold" : "text-rose-500/50"}>
            {failureRate === "-" ? "—" : failureRate}
          </span>
        </td>
        <td className="p-2.5 align-middle text-muted-foreground font-medium w-24">
          {node.probe_total}
        </td>
        <td
          className="p-2.5 text-right align-middle w-20"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-end gap-1">
            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0 text-muted-foreground hover:text-foreground"
              title={t("Test")}
              onClick={props.onTest}
              disabled={props.testing}
            >
              {props.testing ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Play className="size-3.5" />
              )}
            </Button>
            <Button
              size="xs"
              variant="ghost"
              className="size-7 p-0 text-destructive/70 hover:text-destructive hover:bg-destructive/10"
              title={t("Delete")}
              onClick={props.onDelete}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        </td>
      </tr>
      {expanded && (
        <tr className="bg-muted/15 border-b" onClick={(e) => e.stopPropagation()}>
          <td colSpan={8} className="p-3.5">
            <div className="rounded-lg border bg-background/95 p-3.5 space-y-3 shadow-xs">
              {/* 第一行：红色错误信息（若有） */}
              {node.last_error && (
                <div className="rounded-md border border-destructive/20 bg-destructive/10 px-2.5 py-1.5 text-xs text-destructive flex items-center justify-between">
                  <span className="font-medium">{node.last_error}</span>
                  {node.last_probe_at && (
                    <span className="text-[11px] opacity-75 font-mono">
                      {new Date(node.last_probe_at).toLocaleString()}
                    </span>
                  )}
                </div>
              )}

              {/* 第二行：代理链接输入框 */}
              <div className="space-y-1.5">
                <label className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
                  {t("Proxy Link")}
                </label>
                <Input
                  value={proxyLink}
                  onChange={(e) => setProxyLink(e.target.value)}
                  placeholder="socks5://user:pass@host:port"
                  className="font-mono text-xs h-8 bg-muted/20"
                />
              </div>
              {/* 第三行：作用域 (模型别名与渠道选择器) */}
              <div>
                <RouteModelPicker
                  selectedIds={selectedMappingIds}
                  onToggle={handleToggleMapping}
                  pageSize={18}
                />
              </div>

              {/* 保存操作按钮 */}
              <div className="flex justify-end pt-1">
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={updateMutation.isPending}
                >
                  {updateMutation.isPending ? (
                    <Loader2 className="size-3.5 animate-spin mr-1.5" />
                  ) : null}
                  {t("Save Changes")}
                </Button>
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
