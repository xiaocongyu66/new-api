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
import { formatBillingCurrencyFromUSD } from "@/lib/currency";

import { TOKEN_UNIT_DIVISORS } from "../constants";
import type { PricingModel, TokenUnit } from "../types";
import {
  BILLING_PRICING_VARS,
  parseTiersFromExpr,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type BillingVar,
  type ParsedTier,
} from "./billing-expr";
import { getDisplayGroupRatio } from "./model-helpers";

type DynamicPriceOptions = {
  tokenUnit: TokenUnit;
  showRechargePrice?: boolean;
  priceRate?: number;
  usdExchangeRate?: number;
  groupRatioMultiplier?: number;
};

export type DynamicPriceEntry = {
  key: string;
  field: string;
  label: string;
  shortLabel: string;
  value: number;
  formatted: string;
  variable: BillingVar;
};

export type DynamicPricingSummary = {
  tiers: ParsedTier[];
  tier: ParsedTier | null;
  tierCount: number;
  hasRequestRules: boolean;
  isSpecialExpression: boolean;
  rawExpression: string;
  entries: DynamicPriceEntry[];
  primaryEntries: DynamicPriceEntry[];
  secondaryEntries: DynamicPriceEntry[];
};

const PRIMARY_DYNAMIC_FIELDS = new Set(["inputPrice", "outputPrice"]);

export function isDynamicPricingModel(model: PricingModel): boolean {
  return model.billing_mode === "tiered_expr" && Boolean(model.billing_expr);
}

export function getDynamicDisplayGroupRatio(
  model: PricingModel,
  selectedGroup?: string,
): number {
  return getDisplayGroupRatio(model, selectedGroup);
}

function applyRechargeRate(
  price: number,
  showWithRecharge: boolean,
  priceRate: number,
  usdExchangeRate: number,
): number {
  if (!showWithRecharge) return price;
  return (price * priceRate) / usdExchangeRate;
}

function formatDynamicUnitPrice(
  valuePerMillionTokens: number,
  options: DynamicPriceOptions,
): string {
  const groupRatio = options.groupRatioMultiplier ?? 1;
  const priceRate = options.priceRate ?? 1;
  const usdExchangeRate = options.usdExchangeRate ?? 1;
  const priceUSD =
    (valuePerMillionTokens * groupRatio) /
    TOKEN_UNIT_DIVISORS[options.tokenUnit];
  const displayPrice = applyRechargeRate(
    priceUSD,
    options.showRechargePrice ?? false,
    priceRate,
    usdExchangeRate,
  );

  return formatBillingCurrencyFromUSD(displayPrice, {
    digitsLarge: 4,
    digitsSmall: 6,
    abbreviate: false,
  });
}

export function getDynamicPricingTiers(model: PricingModel): ParsedTier[] {
  if (!isDynamicPricingModel(model)) return [];
  const { billingExpr } = splitBillingExprAndRequestRules(
    model.billing_expr || "",
  );
  return parseTiersFromExpr(billingExpr);
}

function hasDynamicRequestRules(model: PricingModel): boolean {
  if (!isDynamicPricingModel(model)) return false;
  const { requestRuleExpr } = splitBillingExprAndRequestRules(
    model.billing_expr || "",
  );
  return Boolean(tryParseRequestRuleExpr(requestRuleExpr || "")?.length);
}

/** Compact token count for tier bounds: 200000 → "200K", 1500000 → "1.5M". */
export function formatTokenCountHint(value: string | number): string {
  const n = Number(value);
  if (!Number.isFinite(n) || n === 0) return "";
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
  }
  if (n >= 1000) {
    return `${(n / 1000).toFixed(n % 1000 === 0 ? 0 : 1)}K`;
  }
  return String(n);
}

const TIER_BOUND_OPS: Record<string, string> = {
  "<": "<",
  "<=": "≤",
  ">": ">",
  ">=": "≥",
};

/**
 * Short context bound for a tier column header, e.g. "≤10K". A trailing
 * catch-all tier has no condition of its own, so it reads as "above the
 * previous tier's bound".
 */
export function getTierBoundLabel(tiers: ParsedTier[], index: number): string {
  const condition = tiers[index]?.conditions[0];
  if (condition) {
    return `${TIER_BOUND_OPS[condition.op] || condition.op}${formatTokenCountHint(condition.value) || condition.value}`;
  }
  const previous = tiers[index - 1]?.conditions[0];
  if (!previous || (previous.op !== "<" && previous.op !== "<=")) return "";
  const op = previous.op === "<" ? "≥" : ">";
  return `${op}${formatTokenCountHint(previous.value) || previous.value}`;
}

export function getDynamicPriceEntries(
  tier: ParsedTier | null,
  options: DynamicPriceOptions,
): DynamicPriceEntry[] {
  if (!tier) return [];

  return BILLING_PRICING_VARS.flatMap((variable) => {
    if (!variable.field) return [];
    const value = Number(tier[variable.field]);
    if (!Number.isFinite(value) || value <= 0) return [];

    return [
      {
        key: variable.key,
        field: variable.field,
        label: variable.label,
        shortLabel: variable.shortLabel,
        value,
        formatted: formatDynamicUnitPrice(value, options),
        variable,
      },
    ];
  }).sort((a, b) => {
    const aPrimary = PRIMARY_DYNAMIC_FIELDS.has(a.field);
    const bPrimary = PRIMARY_DYNAMIC_FIELDS.has(b.field);
    if (aPrimary !== bPrimary) return aPrimary ? -1 : 1;
    return 0;
  });
}

export function getDynamicPricingSummary(
  model: PricingModel,
  options: DynamicPriceOptions,
): DynamicPricingSummary | null {
  if (!isDynamicPricingModel(model)) return null;

  const tiers = getDynamicPricingTiers(model);
  const tier = tiers[0] || null;
  const entries = getDynamicPriceEntries(tier, options);
  const rawExpression = model.billing_expr || "";

  return {
    tiers,
    tier,
    tierCount: tiers.length,
    hasRequestRules: hasDynamicRequestRules(model),
    isSpecialExpression: rawExpression.trim().length > 0 && tiers.length === 0,
    rawExpression,
    entries,
    primaryEntries: entries.filter((entry) =>
      PRIMARY_DYNAMIC_FIELDS.has(entry.field),
    ),
    secondaryEntries: entries.filter(
      (entry) => !PRIMARY_DYNAMIC_FIELDS.has(entry.field),
    ),
  };
}
