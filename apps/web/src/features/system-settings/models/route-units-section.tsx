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
import { useState, useEffect, useRef, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Combobox } from "@/components/ui/combobox";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";

import { SettingsSection } from "../components/settings-section";
import {
  getRouteUnitAliases,
  getRouteUnits,
  updateRouteUnit,
  getDispatchPolicy,
  updateDispatchPolicy,
  getContextRoutes,
  replaceContextRoutes,
  type RouteUnitAliasSummary,
  type RouteUnitView,
  type DispatchMode,
  type DispatchPolicy,
  type ContextRoute,
} from "./api-route-units";
import { NumericSpinnerInput } from "@/features/channels/components/numeric-spinner-input";
import { formatLatency } from "@/features/performance-metrics/lib/format";

interface RouteUnitsSectionProps {
  // No defaultValues needed - data comes from API
}

interface ContextIntervalDraft {
  min: number;
  max: number;
  enabled: boolean;
  selectedUnitIds: number[];
}

export function RouteUnitsSection({}: RouteUnitsSectionProps) {
  const { t } = useTranslation();
  const [aliases, setAliases] = useState<RouteUnitAliasSummary[]>([]);
  const [selectedAlias, setSelectedAlias] = useState<string>("");
  const [routeUnits, setRouteUnits] = useState<RouteUnitView[]>([]);
  const [loadingAliases, setLoadingAliases] = useState(true);
  const [loadingUnits, setLoadingUnits] = useState(false);
  const [savingIds, setSavingIds] = useState<Set<number>>(new Set());
  // Dispatch policy / context-range state
  const [dispatchMode, setDispatchMode] =
    useState<DispatchMode>("weighted_pool");
  const [fallbackAlias, setFallbackAlias] = useState<string>("");
  const [policyLoading, setPolicyLoading] = useState(false);
  const [contextRoutes, setContextRoutes] = useState<ContextRoute[]>([]);
  const [contextLoading, setContextLoading] = useState(false);
  const [savingPolicy, setSavingPolicy] = useState(false);
  const [savingContext, setSavingContext] = useState(false);
  const [drafts, setDrafts] = useState<ContextIntervalDraft[]>([]);
  // Load aliases on mount
  useEffect(() => {
    const loadAliases = async () => {
      try {
        const data = await getRouteUnitAliases();
        setAliases(data);
        if (data.length > 0 && !selectedAlias) {
          setSelectedAlias(data[0].alias);
        }
      } catch (error) {
        toast.error(t("Failed to load route unit aliases"));
        console.error(error);
      } finally {
        setLoadingAliases(false);
      }
    };
    loadAliases();
  }, [t]);

  // Load route units when alias changes, or when the operator asks for fresh
  // runtime numbers. The EWMA and share figures move with live traffic, so a
  // reload is the only way to see the effect of a weight change.
  const [reloadToken, setReloadToken] = useState(0);
  useEffect(() => {
    if (!selectedAlias) {
      setRouteUnits([]);
      return;
    }

    let cancelled = false;
    const loadUnits = async () => {
      setLoadingUnits(true);
      try {
        const data = await getRouteUnits(selectedAlias);
        if (!cancelled) {
          setRouteUnits(data.items);
        }
      } catch (error) {
        if (!cancelled) {
          toast.error(t("Failed to load route units"));
          console.error(error);
        }
      } finally {
        if (!cancelled) {
          setLoadingUnits(false);
        }
      }
    };
    loadUnits();
    return () => {
      cancelled = true;
    };
  }, [selectedAlias, reloadToken, t]);

  // Load dispatch policy and context routes when alias changes so the strategy
  // selector and interval editor reflect the persisted configuration.
  useEffect(() => {
    if (!selectedAlias) {
      setContextRoutes([]);
      setDrafts([]);
      setDispatchMode("weighted_pool");
      setFallbackAlias("");
      return;
    }

    let cancelled = false;
    const loadPolicyAndRoutes = async () => {
      setPolicyLoading(true);
      setContextLoading(true);
      try {
        const [policy, routesResp] = await Promise.all([
          getDispatchPolicy(selectedAlias),
          getContextRoutes(selectedAlias),
        ]);
        if (cancelled) return;
        setDispatchMode((policy.mode as DispatchMode) || "weighted_pool");
        setFallbackAlias(policy.fallback_alias ?? "");
        setContextRoutes(routesResp?.items ?? []);
      } catch (error) {
        if (!cancelled) {
          toast.error(t("Failed to load dispatch policy"));
          console.error(error);
        }
      } finally {
        if (!cancelled) {
          setPolicyLoading(false);
          setContextLoading(false);
        }
      }
    };
    loadPolicyAndRoutes();
    return () => {
      cancelled = true;
    };
  }, [selectedAlias, t]);

  // Reconstruct interval drafts whenever context routes or route units change.
  useEffect(() => {
    const groups = new Map<
      string,
      {
        min: number;
        max: number;
        allEnabled: boolean;
        anyEnabled: boolean;
        ids: Set<number>;
      }
    >();
    for (const r of contextRoutes) {
      const key = `${r.context_min}:${r.context_max}`;
      if (!groups.has(key)) {
        groups.set(key, {
          min: r.context_min,
          max: r.context_max,
          allEnabled: true,
          anyEnabled: false,
          ids: new Set<number>(),
        });
      }
      const g = groups.get(key)!;
      const unit = routeUnits.find(
        (u) =>
          u.channel_id === r.channel_id &&
          u.key_index === r.key_index &&
          u.upstream_model === r.upstream_model,
      );
      if (unit) g.ids.add(unit.id);
      if (!r.enabled) g.allEnabled = false;
      if (r.enabled) g.anyEnabled = true;
    }
    const nextDrafts: ContextIntervalDraft[] = Array.from(groups.values()).map(
      (g) => ({
        min: g.min,
        max: g.max,
        enabled: g.ids.size === 0 ? g.anyEnabled : g.allEnabled,
        selectedUnitIds: Array.from(g.ids),
      }),
    );
    setDrafts(nextDrafts);
  }, [contextRoutes, routeUnits]);

  // Alias options for the searchable selector. With dozens of aliases a plain
  // dropdown is unusable, so the label carries the route count and the combobox
  // filters by typing.
  const aliasOptions = useMemo(
    () =>
      aliases.map((alias) => ({
        value: alias.alias,
        label: `${alias.alias} · ${alias.route_count} ${
          alias.route_count === 1 ? t("route") : t("routes")
        }`,
      })),
    [aliases, t],
  );

  // Live totals for the selected alias, computed from the rows on screen so they
  // always agree with what is displayed. A route weighing 10x+ below the pool's
  // strongest peer is almost always a stray edit: it keeps serving but its share
  // collapses toward zero, which looks like an outage from the outside.
  const summary = useMemo(() => {
    const enabled = routeUnits.filter((u) => u.enabled);
    const poolMaxWeight = enabled.reduce(
      (max, u) => Math.max(max, u.static_weight),
      0,
    );
    return {
      total: routeUnits.length,
      enabled: enabled.length,
      totalWeight: enabled.reduce((sum, u) => sum + u.static_weight, 0),
      degraded: routeUnits.filter((u) => u.enabled && u.health_multiplier < 1)
        .length,
      unsampled: routeUnits.filter((u) => u.sample_count === 0).length,
      lopsided: new Set(
        enabled
          .filter(
            (u) => poolMaxWeight > 0 && u.static_weight * 10 <= poolMaxWeight,
          )
          .map((u) => u.id),
      ),
    };
  }, [routeUnits]);

  const handleWeightChange = async (id: number, newWeight: number) => {
    // Capture original weight for potential rollback
    const unit = routeUnits.find((u) => u.id === id);
    if (!unit) return;
    const originalWeight = unit.static_weight;

    // Debounce: only send PUT after 500ms of no further changes for this id
    const existingTimer = debounceTimers.current.get(id);
    if (existingTimer) {
      clearTimeout(existingTimer);
    }

    // Optimistic update immediately
    setRouteUnits((prev) =>
      prev.map((u) => (u.id === id ? { ...u, static_weight: newWeight } : u)),
    );

    // Mark as saving
    setSavingIds((prev) => new Set(prev).add(id));

    const timer = setTimeout(async () => {
      debounceTimers.current.delete(id);
      try {
        await updateRouteUnit(id, { static_weight: newWeight });
        toast.success(t("Weight updated successfully"));
      } catch (error) {
        // Rollback only the affected row using captured original weight
        setRouteUnits((prev) =>
          prev.map((u) =>
            u.id === id ? { ...u, static_weight: originalWeight } : u,
          ),
        );
        toast.error(t("Failed to update weight"));
        console.error(error);
      } finally {
        setSavingIds((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      }
    }, 500);

    debounceTimers.current.set(id, timer);
  };

  const handleEnabledChange = async (id: number, newEnabled: boolean) => {
    // Optimistic update
    setRouteUnits((prev) =>
      prev.map((u) => (u.id === id ? { ...u, enabled: newEnabled } : u)),
    );

    // Mark as saving
    setSavingIds((prev) => new Set(prev).add(id));

    try {
      await updateRouteUnit(id, { enabled: newEnabled });
      toast.success(t("Status updated successfully"));
    } catch (error) {
      // Rollback only the affected row
      setRouteUnits((prev) =>
        prev.map((u) => (u.id === id ? { ...u, enabled: u.enabled } : u)),
      );
      toast.error(t("Failed to update status"));
      console.error(error);
    } finally {
      setSavingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  };

  const debounceTimers = useRef<Map<number, NodeJS.Timeout>>(new Map());

  // Flush pending debounced updates on unmount
  useEffect(() => {
    return () => {
      debounceTimers.current.forEach((timer) => clearTimeout(timer));
      debounceTimers.current.clear();
    };
  }, []);

  const handleSavePolicy = async () => {
    if (!selectedAlias) return;
    setSavingPolicy(true);
    try {
      const fb = fallbackAlias.trim() === "" ? null : fallbackAlias.trim();
      const data = await updateDispatchPolicy(selectedAlias, dispatchMode, fb);
      setDispatchMode((data.mode as DispatchMode) || dispatchMode);
      setFallbackAlias(data.fallback_alias ?? "");
      toast.success(t("Dispatch policy saved"));
    } catch (error) {
      toast.error(t("Failed to save dispatch policy"));
      console.error(error);
    } finally {
      setSavingPolicy(false);
    }
  };

  const handleSaveContext = async () => {
    if (!selectedAlias) return;
    setSavingContext(true);
    try {
      const items: ContextRoute[] = [];
      drafts.forEach((draft, intervalIdx) => {
        draft.selectedUnitIds.forEach((unitId, unitIdx) => {
          const unit = routeUnits.find((u) => u.id === unitId);
          if (!unit) return;
          items.push({
            id: 0,
            alias: selectedAlias,
            context_min: Number.isFinite(draft.min) ? draft.min : 0,
            context_max: Number.isFinite(draft.max) ? draft.max : 0,
            channel_id: unit.channel_id,
            key_index: unit.key_index,
            upstream_model: unit.upstream_model,
            static_weight: unit.static_weight,
            enabled: draft.enabled,
            sort_order: intervalIdx * 100 + unitIdx,
          });
        });
      });
      const data = await replaceContextRoutes(selectedAlias, items);
      setContextRoutes(data?.items ?? items);
      toast.success(t("Context routes saved"));
    } catch (error) {
      toast.error(t("Failed to save context routes"));
      console.error(error);
    } finally {
      setSavingContext(false);
    }
  };

  const addInterval = () => {
    setDrafts((prev) => [
      ...prev,
      { min: 0, max: 0, enabled: true, selectedUnitIds: [] },
    ]);
  };

  const removeInterval = (index: number) => {
    setDrafts((prev) => prev.filter((_, i) => i !== index));
  };

  const updateDraft = (index: number, patch: Partial<ContextIntervalDraft>) => {
    setDrafts((prev) =>
      prev.map((d, i) => (i === index ? { ...d, ...patch } : d)),
    );
  };

  const toggleUnit = (index: number, unitId: number) => {
    setDrafts((prev) =>
      prev.map((d, i) => {
        if (i !== index) return d;
        const selected = d.selectedUnitIds.includes(unitId);
        return {
          ...d,
          selectedUnitIds: selected
            ? d.selectedUnitIds.filter((id) => id !== unitId)
            : [...d.selectedUnitIds, unitId],
        };
      }),
    );
  };

  const isSaving = (id: number) => savingIds.has(id);

  const channelStatusLabel = (status: number) => {
    switch (status) {
      case 1:
        return t("Active");
      case 2:
        return t("Disabled");
      default:
        return t("Unknown");
    }
  };

  const channelStatusClass = (status: number) => {
    switch (status) {
      case 1:
        return "text-green-600 dark:text-green-400";
      case 2:
        return "text-red-600 dark:text-red-400";
      default:
        return "text-muted-foreground";
    }
  };

  // Quality is clamped to [0.5, 1.5] and sits at a neutral 1.0 until the route has
  // enough samples, so an unsampled route must not be coloured as if it were fine.
  const qualityClass = (unit: RouteUnitView) => {
    if (unit.sample_count === 0) return "text-muted-foreground";
    if (unit.ewma_quality < 0.9) return "text-red-600 dark:text-red-400";
    if (unit.ewma_quality > 1.1) return "text-green-600 dark:text-green-400";
    return undefined;
  };

  // The health multiplier comes from the unit health state machine: 1.0 for a
  // settled route, the EWMA score times its slow-start ramp while recovering,
  // and 0 while cooling or terminally disabled. Naming the state is what
  // makes a low final score explainable.
  const healthLabel = (multiplier: number) => {
    if (multiplier <= 0) return t("Isolated");
    if (multiplier < 1) return t("Degraded");
    return t("Healthy");
  };

  const healthClass = (multiplier: number) => {
    if (multiplier <= 0) return "text-red-600 dark:text-red-400";
    if (multiplier < 1) return "text-amber-600 dark:text-amber-400";
    return "text-green-600 dark:text-green-400";
  };

  // Each route unit reports its own TTFT, so the unit follows its value
  // (seconds once it reaches a second) — same rule as the shared
  // formatLatency, which also renders a dash for unmeasured values.

  if (loadingAliases) {
    return (
      <SettingsSection title={t("Route Units")}>
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          <span className="ml-2 text-muted-foreground">
            {t("Loading aliases...")}
          </span>
        </div>
      </SettingsSection>
    );
  }

  if (aliases.length === 0) {
    return (
      <SettingsSection title={t("Route Units")}>
        <div className="text-center py-12 text-muted-foreground">
          {t("No route unit aliases found")}
        </div>
      </SettingsSection>
    );
  }

  return (
    <SettingsSection title={t("Route Units")}>
      <div className="space-y-6 rounded-xl bg-card-surface p-4 backdrop-blur-card">
        {/* Alias selector: pick a public model alias, then inspect and tune the
            scheduling models competing inside it. */}
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex min-w-[320px] flex-col gap-1.5">
            <label htmlFor="route-unit-alias" className="text-sm font-medium">
              {t("Public Model Alias")}
            </label>
            <Combobox
              options={aliasOptions}
              value={selectedAlias}
              onValueChange={(value) => value && setSelectedAlias(value)}
              placeholder={t("Select an alias")}
              searchPlaceholder={t("Search model alias...")}
              emptyText={t("No matching alias found.")}
              openOnFocus={false}
            />
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setReloadToken((token) => token + 1)}
            disabled={!selectedAlias || loadingUnits}
          >
            <RefreshCw
              className={loadingUnits ? "h-4 w-4 animate-spin" : "h-4 w-4"}
            />
            {t("Refresh")}
          </Button>
        </div>

        {selectedAlias && (
          <>
            {/* Dispatch strategy selector */}
            <div className="flex flex-wrap items-end gap-4">
              <div className="flex min-w-[220px] flex-col gap-1.5">
                <Label htmlFor="dispatch-mode">{t("Dispatch mode")}</Label>
                <Select
                  value={dispatchMode}
                  onValueChange={(value) => {
                    if (value === null) return;
                    setDispatchMode(value as DispatchMode);
                  }}
                >
                  <SelectTrigger id="dispatch-mode">
                    <SelectValue placeholder={t("Select dispatch mode")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="weighted_pool">
                      {t("Weighted pool")}
                    </SelectItem>
                    <SelectItem value="context_range">
                      {t("Context range")}
                    </SelectItem>
                    <SelectItem value="delegate">{t("Delegate")}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {policyLoading && (
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              )}
              <Button
                type="button"
                size="sm"
                onClick={handleSavePolicy}
                disabled={savingPolicy || !selectedAlias}
              >
                {savingPolicy && (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                )}
                {t("Save policy")}
              </Button>
            </div>

            {dispatchMode !== "weighted_pool" && (
              <div className="space-y-2">
                <Label htmlFor="fallback-alias">{t("Fallback alias")}</Label>
                <Input
                  id="fallback-alias"
                  value={fallbackAlias}
                  onChange={(e) => setFallbackAlias(e.target.value)}
                  placeholder={t("Fallback alias (optional)")}
                />
              </div>
            )}

            {dispatchMode === "weighted_pool" && (
              <>
                <p className="text-sm text-muted-foreground">
                  {t(
                    "Scheduling models competing inside this alias. Traffic share is weight × quality × health, so a route keeps serving at a reduced share until the state machine disables it.",
                  )}
                </p>

                {/* Summary of the pool as displayed, so the totals cannot disagree
                with the rows below. */}
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant="outline">
                    {t("Routes")}: {summary.enabled}/{summary.total}
                  </Badge>
                  <Badge variant="outline">
                    {t("Total weight")}: {summary.totalWeight}
                  </Badge>
                  {summary.degraded > 0 && (
                    <Badge
                      variant="outline"
                      className="text-amber-600 dark:text-amber-400"
                    >
                      {t("Degraded")}: {summary.degraded}
                    </Badge>
                  )}
                  {summary.unsampled > 0 && (
                    <Badge variant="outline" className="text-muted-foreground">
                      {t("Awaiting samples")}: {summary.unsampled}
                    </Badge>
                  )}
                  {summary.lopsided.size > 0 && (
                    <Badge
                      variant="outline"
                      className="text-amber-600 dark:text-amber-400"
                    >
                      {t("Lopsided weights")}: {summary.lopsided.size}
                    </Badge>
                  )}
                </div>

                <Separator />

                {/* Route Units Table */}
                <div className="overflow-x-auto">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-[40px]">{t("#")}</TableHead>
                        <TableHead>{t("Scheduling Model")}</TableHead>
                        <TableHead className="w-[110px]">
                          {t("Weight")}
                        </TableHead>
                        <TableHead className="w-[90px]">
                          {t("Enabled")}
                        </TableHead>
                        <TableHead className="w-[130px]">
                          {t("Share (target/actual)")}
                        </TableHead>
                        <TableHead className="w-[90px]">
                          {t("Quality")}
                        </TableHead>
                        <TableHead className="w-[110px]">
                          {t("Health")}
                        </TableHead>
                        <TableHead className="w-[150px]">
                          {t("Latency / Throughput")}
                        </TableHead>
                        <TableHead className="w-[130px]">
                          {t("Final Score")}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {loadingUnits ? (
                        <TableRow>
                          <TableCell colSpan={9} className="text-center py-8">
                            <div className="flex items-center justify-center gap-2">
                              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                              <span>{t("Loading route units...")}</span>
                            </div>
                          </TableCell>
                        </TableRow>
                      ) : routeUnits.length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={9}
                            className="text-center py-8 text-muted-foreground"
                          >
                            {t("No route units for this alias")}
                          </TableCell>
                        </TableRow>
                      ) : (
                        routeUnits.map((unit, index) => (
                          <TableRow
                            key={unit.id}
                            className={unit.enabled ? undefined : "opacity-60"}
                          >
                            <TableCell className="font-mono text-muted-foreground">
                              {index + 1}
                            </TableCell>

                            {/* Identity of the scheduling model: which channel, which
                            key, and what it actually calls upstream. */}
                            <TableCell>
                              <div className="flex flex-col gap-0.5">
                                <div className="flex items-center gap-2">
                                  <span className="font-medium">
                                    {unit.channel_name}
                                  </span>
                                  {unit.key_index > 0 && (
                                    <Badge
                                      variant="secondary"
                                      className="font-mono text-[10px]"
                                    >
                                      {t("key")} {unit.key_index}
                                    </Badge>
                                  )}
                                  <span
                                    className={`text-xs ${channelStatusClass(unit.channel_status)}`}
                                  >
                                    {channelStatusLabel(unit.channel_status)}
                                  </span>
                                </div>
                                <span
                                  className="truncate font-mono text-xs text-muted-foreground"
                                  title={
                                    unit.base_url
                                      ? `${unit.upstream_model} — ${unit.base_url}`
                                      : unit.upstream_model
                                  }
                                >
                                  {unit.upstream_model}
                                  {unit.base_url ? ` · ${unit.base_url}` : ""}
                                </span>
                              </div>
                            </TableCell>

                            <TableCell>
                              <div className="flex items-center gap-1.5">
                                <NumericSpinnerInput
                                  value={unit.static_weight}
                                  onChange={(value) =>
                                    handleWeightChange(unit.id, value)
                                  }
                                  min={0}
                                  step={1}
                                  disabled={loadingUnits || isSaving(unit.id)}
                                  className="w-[90px]"
                                />
                                {summary.lopsided.has(unit.id) && (
                                  <span
                                    className="text-amber-600 dark:text-amber-400"
                                    title={t(
                                      "Weight is far below this alias pool maximum, so its traffic share is near zero.",
                                    )}
                                  >
                                    ⚠
                                  </span>
                                )}
                              </div>
                            </TableCell>

                            <TableCell>
                              <Switch
                                checked={unit.enabled}
                                onCheckedChange={(checked) =>
                                  handleEnabledChange(unit.id, checked)
                                }
                                disabled={loadingUnits || isSaving(unit.id)}
                                aria-label={t(
                                  unit.enabled
                                    ? "Disable route unit"
                                    : "Enable route unit",
                                )}
                              />
                            </TableCell>

                            {/* Target share is the operator's configured intent; actual
                            share is what the window measured. Divergence is what the
                            correction is working on. */}
                            <TableCell className="font-mono text-sm">
                              <div className="flex flex-col gap-0.5">
                                <span>
                                  {(unit.expected_share * 100).toFixed(1)}%
                                </span>
                                <span className="text-xs text-muted-foreground">
                                  {unit.share_opportunities > 0
                                    ? `${(unit.actual_share * 100).toFixed(1)}% · ${unit.share_selections}/${unit.share_opportunities}`
                                    : t("no traffic yet")}
                                </span>
                              </div>
                            </TableCell>

                            <TableCell className="font-mono text-sm">
                              <div className="flex flex-col gap-0.5">
                                <span className={qualityClass(unit)}>
                                  {unit.ewma_quality.toFixed(2)}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                  {unit.sample_count > 0
                                    ? `${(unit.success_ewma * 100).toFixed(0)}% ok · n=${unit.sample_count}`
                                    : t("neutral")}
                                </span>
                              </div>
                            </TableCell>

                            {/* The health multiplier is the only factor allowed to reach
                            zero, so its state is spelled out rather than left as a
                            bare number. */}
                            <TableCell className="text-sm">
                              <div className="flex flex-col gap-0.5">
                                <span
                                  className={healthClass(
                                    unit.health_multiplier,
                                  )}
                                >
                                  {healthLabel(unit.health_multiplier)}
                                </span>
                                <span className="font-mono text-xs text-muted-foreground">
                                  ×{unit.health_multiplier.toFixed(2)}
                                </span>
                              </div>
                            </TableCell>

                            <TableCell className="font-mono text-xs text-muted-foreground">
                              {unit.sample_count > 0 ? (
                                <div className="flex flex-col gap-0.5">
                                  <span>
                                    {formatLatency(unit.ttft_ewma_ms)}{" "}
                                    {t("TTFT")}
                                  </span>
                                  <span>
                                    {unit.tps_ewma.toFixed(1)} {t("tok/s")}
                                  </span>
                                </div>
                              ) : (
                                "—"
                              )}
                            </TableCell>

                            {/* Final score with its factorisation, so the number can be
                            checked by hand against the four terms. */}
                            <TableCell className="font-mono text-sm">
                              <div className="flex flex-col gap-0.5">
                                <span>{unit.final_score.toFixed(2)}</span>
                                <span
                                  className="text-xs text-muted-foreground"
                                  title={t(
                                    "base weight × quality × health × share correction",
                                  )}
                                >
                                  {unit.base_weight.toFixed(0)}×
                                  {unit.ewma_quality.toFixed(2)}×
                                  {unit.health_multiplier.toFixed(2)}×
                                  {unit.share_correction.toFixed(2)}
                                </span>
                              </div>
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </div>
              </>
            )}

            {dispatchMode === "context_range" && (
              <div className="space-y-4">
                <div className="flex items-center justify-between gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={addInterval}
                  >
                    {t("Add interval")}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    onClick={handleSaveContext}
                    disabled={savingContext || contextLoading}
                  >
                    {savingContext && (
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    )}
                    {t("Save intervals")}
                  </Button>
                </div>

                {contextLoading && (
                  <div className="flex items-center justify-center py-8">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                  </div>
                )}

                {!contextLoading && drafts.length === 0 && (
                  <div className="text-center py-8 text-muted-foreground">
                    {t("No context intervals configured")}
                  </div>
                )}

                {drafts.map((draft, index) => (
                  <div key={index} className="space-y-3 rounded-lg border p-4">
                    <div className="flex flex-wrap items-end gap-3">
                      <div className="flex flex-col gap-1.5">
                        <Label>{t("Min tokens")}</Label>
                        <Input
                          type="number"
                          min={0}
                          value={draft.min}
                          onChange={(e) =>
                            updateDraft(index, {
                              min: Number(e.target.value) || 0,
                            })
                          }
                          className="w-[140px]"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <Label>{t("Max tokens (0 = unbounded)")}</Label>
                        <Input
                          type="number"
                          min={0}
                          value={draft.max}
                          onChange={(e) =>
                            updateDraft(index, {
                              max: Number(e.target.value) || 0,
                            })
                          }
                          className="w-[180px]"
                        />
                      </div>
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={draft.enabled}
                          onCheckedChange={(checked) =>
                            updateDraft(index, { enabled: checked })
                          }
                        />
                        <span className="text-sm">{t("Enabled")}</span>
                      </div>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => removeInterval(index)}
                      >
                        {t("Remove")}
                      </Button>
                    </div>

                    <div className="space-y-1.5">
                      <Label>{t("Included route units")}</Label>
                      {routeUnits.length === 0 ? (
                        <div className="text-sm text-muted-foreground">
                          {t("No route units for this alias")}
                        </div>
                      ) : (
                        <div className="space-y-1.5">
                          {routeUnits.map((unit) => (
                            <label
                              key={unit.id}
                              className="flex items-center gap-2 text-sm"
                            >
                              <Checkbox
                                checked={draft.selectedUnitIds.includes(
                                  unit.id,
                                )}
                                onCheckedChange={() =>
                                  toggleUnit(index, unit.id)
                                }
                              />
                              <span>
                                {unit.channel_name} · {unit.upstream_model}
                              </span>
                            </label>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}

            {dispatchMode === "delegate" && (
              <p className="text-sm text-muted-foreground">
                {t(
                  "Delegate mode forwards routing to the configured fallback alias.",
                )}
              </p>
            )}
          </>
        )}
      </div>
    </SettingsSection>
  );
}
