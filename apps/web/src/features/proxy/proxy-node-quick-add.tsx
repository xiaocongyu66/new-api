import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus, TestTube2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/hooks/use-toast";

import { createProxyNode } from "./proxy-node-api";
import type { ProxyNodeRequest } from "./proxy-node-types";

const createDefaultForm = (): ProxyNodeRequest => ({
  name: "",
  enabled: true,
  proxy: "",
  scope_type: "all",
  scope_value: undefined,
});

export function ProxyNodeQuickAdd(props: { onAdded: () => void }) {
  const { t } = useTranslation();
  const [form, setForm] = useState<ProxyNodeRequest>(createDefaultForm);

  const mutation = useMutation({
    mutationFn: (payload: ProxyNodeRequest) => createProxyNode(payload),
    onSuccess: () => {
      setForm(createDefaultForm());
      props.onAdded();
      toast.success(t("Saved successfully"));
    },
    onError: (error: Error) =>
      toast.error(error.message || t("Failed to save")),
  });

  const canSubmit =
    !mutation.isPending &&
    form.name.trim() !== "" &&
    (form.proxy ?? "").trim() !== "";

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("Quick Add Proxy Node")}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 lg:grid-cols-12">
          {/* Left col ≥50% */}
          <div className="space-y-3 lg:col-span-7">
            {/* Top row: test + enable buttons (left) + Name input (right) */}
            <div className="grid grid-cols-1 items-start gap-3 sm:grid-cols-[auto_auto_1fr] sm:items-center">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  /* ponytail: test-button wiring deferred; reserved slot for upcoming channel/probe integration */
                }}
              >
                <TestTube2 className="size-4" />
                {t("Test")}
              </Button>
              <Button
                type="button"
                variant={form.enabled ? "default" : "outline"}
                size="sm"
                onClick={() => setForm({ ...form, enabled: !form.enabled })}
              >
                {form.enabled ? t("Enabled") : t("Disabled")}
              </Button>
              <Input
                id="quick-add-name"
                value={form.name}
                placeholder={t("Proxy node name")}
                onChange={(event) =>
                  setForm({ ...form, name: event.target.value })
                }
              />
            </div>

            {/* Socks5 link as Textarea */}
            <Textarea
              id="quick-add-proxy"
              value={form.proxy ?? ""}
              placeholder="socks5://…"
              rows={3}
              onChange={(event) =>
                setForm({ ...form, proxy: event.target.value })
              }
            />

            {/* Submit row */}
            <div className="flex justify-end">
              <Button
                onClick={() => {
                  mutation.mutate({
                    ...form,
                    name: form.name.trim(),
                    proxy: (form.proxy ?? "").trim(),
                  });
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

          {/* Right col ≥40% — Tabs only, no outer Label */}
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
