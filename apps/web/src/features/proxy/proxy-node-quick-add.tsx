import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "@/hooks/use-toast";

import { createProxyNode } from "./proxy-node-api";
import { proxyNodeDefaultScopeValue } from "./proxy-node-scope";
import type { ProxyNode, ProxyNodeRequest } from "./proxy-node-types";

type ScopeType = ProxyNode["scope_type"];

const DEFAULT_FORM: ProxyNodeRequest = {
  name: "",
  enabled: true,
  proxy: "",
  scope_type: "all",
  scope_value: undefined,
};

export function ProxyNodeQuickAdd(props: {
  channels: Array<{ id: number; name: string }>;
  groups: string[];
  onAdded: () => void;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<ProxyNodeRequest>(DEFAULT_FORM);

  const mutation = useMutation({
    mutationFn: (payload: ProxyNodeRequest) => createProxyNode(payload),
    onSuccess: () => {
      setForm({ ...DEFAULT_FORM });
      props.onAdded();
      toast.success(t("Saved successfully"));
    },
    onError: (error: Error) =>
      toast.error(error.message || t("Failed to save")),
  });

  const isAll = form.scope_type === "all";

  // Auto-fill scope_value when scope_type changes (mirror ProxyNodeEditor UX).
  // Functional setter avoids stale closure on `form`.
  useEffect(() => {
    if (isAll) return;
    setForm((prev) => {
      if (prev.scope_value) return prev;
      const scopeValue = proxyNodeDefaultScopeValue(
        prev.scope_type,
        props.channels,
        props.groups,
      );
      return scopeValue ? { ...prev, scope_value: scopeValue } : prev;
    });
  }, [form.scope_type, isAll, props.channels, props.groups]);

  const canSubmit =
    !mutation.isPending &&
    form.name.trim() !== "" &&
    (form.proxy ?? "").trim() !== "" &&
    (isAll || (form.scope_value ?? "").trim() !== "");

  const renderScopeField = () => {
    if (form.scope_type === "channel" && props.channels.length > 0) {
      return (
        <Select
          value={form.scope_value ?? ""}
          onValueChange={(value) =>
            setForm({ ...form, scope_value: value || undefined })
          }
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {props.channels.map((channel) => (
              <SelectItem key={channel.id} value={String(channel.id)}>
                {channel.name} (#{channel.id})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
    }
    if (form.scope_type === "group" && props.groups.length > 0) {
      return (
        <Select
          value={form.scope_value ?? ""}
          onValueChange={(value) =>
            setForm({ ...form, scope_value: value || undefined })
          }
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {props.groups.map((group) => (
              <SelectItem key={group} value={group}>
                {group}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
    }
    const placeholder =
      form.scope_type === "channel"
        ? t("No channels available")
        : t("No groups available");
    return (
      <Input
        value={form.scope_value ?? ""}
        placeholder={placeholder}
        onChange={(event) =>
          setForm({ ...form, scope_value: event.target.value })
        }
      />
    );
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("Quick Add Proxy Node")}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 lg:grid-cols-12">
          {/* Left: name / scope / enabled / proxy link (≥50%) */}
          <div className="space-y-3 lg:col-span-7">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="quick-add-name">{t("Name")}</Label>
                <Input
                  id="quick-add-name"
                  value={form.name}
                  placeholder={t("Proxy node name")}
                  onChange={(event) =>
                    setForm({ ...form, name: event.target.value })
                  }
                />
              </div>
              <div className="space-y-1.5">
                <Label>{t("Scope Type")}</Label>
                <Select
                  value={form.scope_type}
                  onValueChange={(value) => {
                    if (
                      value === "all" ||
                      value === "channel" ||
                      value === "group"
                    ) {
                      setForm({ ...form, scope_type: value as ScopeType });
                    }
                  }}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="all">{t("All Channels")}</SelectItem>
                      <SelectItem value="channel">
                        {t("Specific Channel")}
                      </SelectItem>
                      <SelectItem value="group">{t("By Group")}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {!isAll && (
              <div className="space-y-1.5">
                <Label>
                  {form.scope_type === "channel"
                    ? t("Channel ID")
                    : t("Group Name")}
                </Label>
                {renderScopeField()}
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="quick-add-proxy">{t("Proxy Link")}</Label>
              <Input
                id="quick-add-proxy"
                value={form.proxy ?? ""}
                placeholder="socks5://…"
                onChange={(event) =>
                  setForm({ ...form, proxy: event.target.value })
                }
              />
            </div>

            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2">
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(enabled) =>
                    setForm({ ...form, enabled })
                  }
                />
                <Label>{t("Enabled")}</Label>
              </div>
              <Button
                onClick={() => {
                  const payload: ProxyNodeRequest = {
                    name: form.name.trim(),
                    enabled: form.enabled,
                    proxy: (form.proxy ?? "").trim(),
                    scope_type: form.scope_type,
                    scope_value:
                      form.scope_type === "all"
                        ? undefined
                        : form.scope_value,
                  };
                  mutation.mutate(payload);
                }}
                disabled={!canSubmit}
              >
                {mutation.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Plus className="size-4" />
                )}
                {t("Add")}
              </Button>
            </div>
          </div>

          {/* Right: channel placeholder sub-panel (≥40%) */}
          <div className="space-y-1.5 lg:col-span-5">
            <Label>{t("Channel")}</Label>
            <Tabs defaultValue="channel">
              <TabsList>
                <TabsTrigger value="channel">{t("Channel")}</TabsTrigger>
                <TabsTrigger value="model">{t("Model")}</TabsTrigger>
              </TabsList>
              <TabsContent value="channel">
                <div className="rounded-md border border-dashed p-6 text-center text-muted-foreground text-sm">
                  {t("Channel picker coming soon")}
                </div>
              </TabsContent>
              <TabsContent value="model">
                <div className="rounded-md border border-dashed p-6 text-center text-muted-foreground text-sm">
                  {t("Model picker coming soon")}
                </div>
              </TabsContent>
            </Tabs>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}