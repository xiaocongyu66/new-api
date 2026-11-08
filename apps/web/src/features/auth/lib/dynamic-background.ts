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
import {
  hexFromArgb,
  sourceColorFromImageBytes,
  themeFromSourceColor,
  type Theme,
} from "@material/material-color-utilities";

/**
 * 壁纸代理接口：前端同源请求 `/api/wallpaper`，后端定时从微雨
 * `https://api.weirain.com/pc` 抓取并缓存，返回图片经过 gzip 压缩。
 * 同源请求无 CORS 问题，且可按需开启 gzip，减少传输体积与加载延迟。
 */
const WALLPAPER_API = "/api/wallpaper";

/**
 * 新版 Scheme 类型才包含 surface-container 系列角色，旧类型没有；
 * 因此通过宽松的 record 读取并逐角色回退，避免对打包版本产生硬依赖。
 */
type SchemeLike = Record<string, number | undefined>;

const role = (scheme: SchemeLike, key: string, fallback: string): string => {
  const value = scheme[key];
  return hexFromArgb(
    typeof value === "number" ? value : (scheme[fallback] as number),
  );
};

type DynamicRoles = {
  light: Record<string, string>;
  dark: Record<string, string>;
};

/** 把 material-color-utilities 的 Theme 映射为本项目的 shadcn 令牌角色。 */
function rolesFromTheme(theme: Theme): DynamicRoles {
  const map = (scheme: SchemeLike): Record<string, string> => ({
    background: role(scheme, "surface", "surface"),
    foreground: role(scheme, "onSurface", "onSurface"),
    card: role(scheme, "surfaceContainerLow", "surface"),
    "card-foreground": role(scheme, "onSurface", "onSurface"),
    popover: role(scheme, "surfaceContainer", "surface"),
    "popover-foreground": role(scheme, "onSurface", "onSurface"),
    primary: role(scheme, "primary", "primary"),
    "primary-foreground": role(scheme, "onPrimary", "onPrimary"),
    secondary: role(scheme, "secondaryContainer", "secondaryContainer"),
    "secondary-foreground": role(scheme, "onSecondaryContainer", "onPrimary"),
    muted: role(scheme, "surfaceContainerHigh", "surfaceVariant"),
    "muted-foreground": role(scheme, "onSurfaceVariant", "onSurfaceVariant"),
    accent: role(scheme, "primaryContainer", "primary"),
    "accent-foreground": role(scheme, "onPrimaryContainer", "onPrimary"),
    destructive: role(scheme, "error", "error"),
    "destructive-foreground": role(scheme, "onError", "onError"),
    border: role(scheme, "outlineVariant", "outline"),
    input: role(scheme, "outlineVariant", "outline"),
    ring: role(scheme, "primary", "primary"),
    sidebar: role(scheme, "surfaceContainerLow", "surface"),
    "sidebar-foreground": role(scheme, "onSurface", "onSurface"),
    "sidebar-accent": role(scheme, "primaryContainer", "primary"),
    "sidebar-accent-foreground": role(
      scheme,
      "onPrimaryContainer",
      "onPrimary",
    ),
    "sidebar-border": role(scheme, "outlineVariant", "outline"),
    "sidebar-ring": role(scheme, "primary", "primary"),
  });

  return {
    light: map(theme.schemes.light as unknown as SchemeLike),
    dark: map(theme.schemes.dark as unknown as SchemeLike),
  };
}

/** 把亮/暗两套角色写入作用域元素（--dyn-light-* / --dyn-dark-*）。 */
function applyDynamicRoles(scope: HTMLElement, roles: DynamicRoles): void {
  for (const [key, value] of Object.entries(roles.light)) {
    scope.style.setProperty(`--dyn-light-${key}`, value);
  }
  for (const [key, value] of Object.entries(roles.dark)) {
    scope.style.setProperty(`--dyn-dark-${key}`, value);
  }
  scope.setAttribute("data-dynamic-theme", "");
}

type BackgroundResource = {
  promise: Promise<HTMLImageElement>;
  cachedImagePromise: Promise<HTMLImageElement> | undefined;
  cachedUrl: string | undefined;
  controller: AbortController;
  subscribers: number;
  settled: boolean;
};

// Root effect replay shares the pending request; successful loads survive navigation.
let backgroundResource: BackgroundResource | undefined;

// The image API returns a fresh random image per call, so the browser HTTP cache
// never reuses a previous background. Persist the last successful image as a
// data URL: the next load paints it instantly and the network fetch degrades
// to a background refresh that may fail without blanking the page.
const CACHED_BACKGROUND_KEY = "app-background-image";
// The upstream serves pre-scaled ~300KB images; base64 is ~0.4MB, so the cap
// leaves the shared ~5MB origin localStorage quota effectively untouched.
const CACHED_BACKGROUND_MAX_LENGTH = 3_500_000;

type CachedBackground = {
  url: string;
  dataUrl: string;
};

function readCachedBackground(): CachedBackground | undefined {
  try {
    const raw = window.localStorage.getItem(CACHED_BACKGROUND_KEY);
    if (!raw) return undefined;
    const parsed: unknown = JSON.parse(raw);
    if (
      typeof parsed === "object" &&
      parsed !== null &&
      typeof (parsed as CachedBackground).url === "string" &&
      typeof (parsed as CachedBackground).dataUrl === "string" &&
      (parsed as CachedBackground).dataUrl.startsWith("data:image/")
    ) {
      return parsed as CachedBackground;
    }
  } catch {
    // Corrupt entry or unavailable storage: fall back to network-only loads.
  }
  return undefined;
}

function writeCachedBackground(entry: CachedBackground): void {
  if (entry.dataUrl.length > CACHED_BACKGROUND_MAX_LENGTH) return;
  try {
    window.localStorage.setItem(CACHED_BACKGROUND_KEY, JSON.stringify(entry));
  } catch {
    // Quota exceeded or storage disabled: the in-memory path still works.
  }
}

/** 把响应体的 blob 转 dataUrl 并落盘缓存，供下次加载即时展示。 */
function persistFreshImageFromBlob(blob: Blob): void {
  void new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () =>
      reject(new Error("Failed to persist background image"));
    reader.readAsDataURL(blob);
  })
    .then((dataUrl) => writeCachedBackground({ url: WALLPAPER_API, dataUrl }))
    .catch(() => {});
}

function loadBackgroundImage(
  src: string,
  signal: AbortSignal,
): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    // 同源请求，无需 crossOrigin；同源图片可被 canvas 读取像素，
    // 支持取色与 dataUrl 缓存。
    image.referrerPolicy = "no-referrer";
    const timeout = window.setTimeout(onError, 12000);

    function cleanup() {
      window.clearTimeout(timeout);
      image.removeEventListener("load", onLoad);
      image.removeEventListener("error", onError);
      signal.removeEventListener("abort", onError);
    }
    function onLoad() {
      cleanup();
      resolve(image);
    }
    function onError() {
      cleanup();
      image.removeAttribute("src");
      reject(new Error("Background image unavailable"));
    }

    image.addEventListener("load", onLoad);
    image.addEventListener("error", onError);
    signal.addEventListener("abort", onError, { once: true });
    if (signal.aborted) {
      onError();
      return;
    }
    image.src = src;
  });
}

/**
 * 可响应 AbortSignal 的睡眠：若在等待期间 signal 被 abort（组件卸载），
 * 立即拒绝，避免白等 1 秒后再发起一注会因 abort 而失败的重试请求。
 */
async function sleepAbortable(ms: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) {
    throw new DOMException("Aborted", "AbortError");
  }
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

/**
 * 从同源壁纸代理接口获取壁纸响应。遇到 429（同一 IP 每秒最多 20 次，
 * 下一秒恢复）时等待 1 秒后重试一次。
 */
async function fetchWallpaper(signal: AbortSignal): Promise<Response> {
  const response = await fetch(WALLPAPER_API, {
    cache: "no-store",
    credentials: "omit",
    referrerPolicy: "no-referrer",
    redirect: "error",
    signal,
  });
  if (response.ok) return response;
  if (response.status === 429) {
    await sleepAbortable(1000, signal);
    const retry = await fetch(WALLPAPER_API, {
      cache: "no-store",
      credentials: "omit",
      referrerPolicy: "no-referrer",
      redirect: "error",
      signal,
    });
    if (retry.ok) return retry;
  }
  throw new Error("Background query unavailable");
}

function createBackgroundResource(): BackgroundResource {
  const controller = new AbortController();
  const cached = readCachedBackground();
  const resource: BackgroundResource = {
    controller,
    cachedUrl: cached?.url,
    cachedImagePromise: cached
      ? loadBackgroundImage(cached.dataUrl, controller.signal)
      : undefined,
    subscribers: 0,
    settled: false,
    promise: (async () => {
      const timeout = window.setTimeout(() => controller.abort(), 12000);
      try {
        const response = await fetchWallpaper(controller.signal);
        window.clearTimeout(timeout);
        // Response body 只能读一次：先拿 blob，既用于展示图片，也用于落盘缓存。
        const blob = await response.blob();
        const url = URL.createObjectURL(blob);
        let image: HTMLImageElement;
        try {
          image = await loadBackgroundImage(url, controller.signal);
        } finally {
          // 图片已由浏览器解码，blob URL 可安全释放，避免 SPA 中长期驻留
          URL.revokeObjectURL(url);
        }
        persistFreshImageFromBlob(blob);
        return image;
      } catch (error) {
        window.clearTimeout(timeout);
        if (controller.signal.aborted)
          throw new Error("Background load cancelled");
        throw error;
      } finally {
        window.clearTimeout(timeout);
      }
    })(),
  };
  void resource.promise.then(
    () => {
      resource.settled = true;
    },
    () => {
      resource.settled = true;
      if (backgroundResource === resource) backgroundResource = undefined;
    },
  );
  return resource;
}

/**
 * Subscribe to one page-lifetime background load. 只交付一张图片：
 * - 有本地缓存时优先用缓存即时展示，避免白屏；
 * - 无缓存时等待网络请求，成功后交付并写入缓存。
 * 网络请求始终在后台进行，成功时只刷新缓存，不再二次交付新图替换页面，
 * 从而彻底消除"先抓缓存图、再抓新图"的双抓取延迟。
 * Optional material color extraction stays available without changing the shared
 * display request.
 */
export function loadRandomBackground(
  scope: HTMLElement,
  onUrl: (url: string, image: HTMLImageElement) => void,
  dynamicColor = false,
): () => void {
  const resource = (backgroundResource ??= createBackgroundResource());
  resource.subscribers += 1;
  let cancelled = false;
  let appliedRoles: DynamicRoles | undefined;
  // Dedupe deliveries so the same image never replays the fade-in.
  let deliveredUrl: string | undefined;

  const deliver = (url: string, image: HTMLImageElement) => {
    if (cancelled || deliveredUrl === url) return;
    deliveredUrl = url;
    onUrl(url, image);
    if (!dynamicColor) return;
    try {
      const canvas = document.createElement("canvas");
      const scale = Math.min(
        1,
        128 / Math.max(image.naturalWidth, image.naturalHeight),
      );
      canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
      canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
      const context = canvas.getContext("2d");
      if (!context) return;
      context.drawImage(image, 0, 0, canvas.width, canvas.height);
      const pixels = context.getImageData(
        0,
        0,
        canvas.width,
        canvas.height,
      ).data;
      appliedRoles = rolesFromTheme(
        themeFromSourceColor(sourceColorFromImageBytes(pixels)),
      );
      applyDynamicRoles(scope, appliedRoles);
    } catch {
      // Unreadable pixels do not discard the background or static theme.
    }
  };

  if (resource.cachedImagePromise) {
    void resource.cachedImagePromise.then(
      (image) => deliver(resource.cachedUrl ?? image.src, image),
      () => {
        // 缓存图已损坏或不可用，fallback 到后台正在飞行的网络图，
        // 避免用户看到空白背景。
        void resource.promise.then(
          (image) => deliver(image.currentSrc || image.src, image),
          () => {},
        );
      },
    );
  } else {
    void resource.promise.then(
      (image) => deliver(image.currentSrc || image.src, image),
      () => {},
    );
  }

  return () => {
    if (cancelled) return;
    cancelled = true;
    resource.subscribers -= 1;
    if (appliedRoles) {
      for (const mode of ["light", "dark"] as const) {
        for (const key of Object.keys(appliedRoles[mode])) {
          scope.style.removeProperty(`--dyn-${mode}-${key}`);
        }
      }
      scope.removeAttribute("data-dynamic-theme");
    }
    // React StrictMode re-subscribes synchronously before this microtask runs.
    queueMicrotask(() => {
      if (resource.subscribers === 0 && !resource.settled) {
        resource.controller.abort();
        if (backgroundResource === resource) backgroundResource = undefined;
      }
    });
  };
}
