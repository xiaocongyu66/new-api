import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus, RefreshCw, TestTube2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/hooks/use-toast";

import { createProxyNodesBatch } from "./proxy-node-api";
import type { ProxyNodeBatchRequest, ProxyNodeBatchResult } from "./proxy-node-types";

const createDefaultBatch = (): ProxyNodeBatchRequest => ({
  name_prefix: "",
  enabled: true,
  proxy_text: "",
  scope_type: "all",
  scope_value: undefined,
});

export function ProxyNodeQuickAdd(props: {
  onAdded: () => void;
  nodesCount: number;
  nodesRefreshing: boolean;
  onRefresh: () => void;
  onOpenAdd: () => void;
  onOpenBatch: () => void;
  onTestAll: () => void;
  testingAll: boolean;
}) {
  const { t } = useTranslation();
  const [batch, setBatch] = useState<ProxyNodeBatchRequest>(createDefaultBatch);

  const mutation = useMutation({
    mutationFn: (payload: ProxyNodeBatchRequest) =>
      createProxyNodesBatch(payload),
    onSuccess: (result: ProxyNodeBatchResult) => {
      setBatch(createDefaultBatch());
      props.onAdded();
      const { created, failed, skipped } = result;
      if (created > 0) {
        toast.success(
          t("Created {{created}}, failed {{failed}}, skipped {{skipped}}", {
            created,
            failed,
            skipped,
          })
        );
      } else if (failed > 0 || skipped > 0) {
        toast.error(
          t("Failed to create any node ({{failed}} failed, {{skipped}} skipped)", {
            failed,
            skipped,
          })
        );
      }
      if (result.errors.length > 0) {
        toast.error(result.errors.join("\n"));
      }
    },
    onError: (error: Error) =>
      toast.error(error.message || t("Batch import failed")),
  });

  const lines = useMemo(() => {
    const seen = new Set<string>();
    return batch.proxy_text
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter((line) => {
        if (!line || line.startsWith("#") || seen.has(line)) return false;
        seen.add(line);
        return true;
      });
  }, [batch.proxy_text]);
  const canSubmit =
    !mutation.isPending && lines.length > 0 && lines.length <= 500;

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle className="text-base">{t("Batch Add")}</CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={props.onRefresh}
            disabled={props.nodesRefreshing}
          >
            <RefreshCw className="size-4" />
            {t("Refresh")}
          </Button>
          <Button type="button" size="sm" onClick={props.onOpenAdd}>
            <Plus className="size-4" />
            {t("Add Proxy Node")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={props.onOpenBatch}
          >
            <Plus className="size-4" />
            {t("Batch Import")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={props.onTestAll}
            disabled={props.nodesCount === 0 || props.testingAll}
          >
            {props.testingAll ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <TestTube2 className="size-4" />
            )}
            {t("Test All Nodes")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled
            title={t("Coming soon")}
          >
            <TestTube2 className="size-4" />
            {t("Test")}
          </Button>
          <Button
            type="button"
            variant={batch.enabled ? "default" : "outline"}
            size="sm"
            onClick={() => setBatch({ ...batch, enabled: !batch.enabled })}
          >
            {batch.enabled ? t("Enabled") : t("Disabled")}
          </Button>
          <Button
            onClick={() => {
              mutation.mutate({
                ...batch,
                name_prefix: batch.name_prefix.trim(),
                proxy_text: batch.proxy_text.trim(),
              });
            }}
            disabled={!canSubmit}
            size="sm"
          >
            {mutation.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
            {t("Add")}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 lg:grid-cols-12">
          <div className="space-y-3 lg:col-span-7">
            <Input
              id="batch-add-name-prefix"
              value={batch.name_prefix}
              placeholder={t("Default: Proxy Node")}
              onChange={(event) =>
                setBatch({ ...batch, name_prefix: event.target.value })
              }
            />
            <Textarea
              id="batch-add-proxy-text"
              value={batch.proxy_text}
              placeholder={"socks5://…\nvmess://…"}
              rows={8}
              onChange={(event) =>
                setBatch({ ...batch, proxy_text: event.target.value })
              }
            />
            <p className="text-muted-foreground text-xs">
              {t("{{count}} unique entries will be created", {
                count: lines.length,
              })}
            </p>
            {lines.length > 500 && (
              <p className="text-destructive text-sm">
                {t("Maximum 500 entries")}
              </p>
            )}
          </div>
          <div className="lg:col-span-5">
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
