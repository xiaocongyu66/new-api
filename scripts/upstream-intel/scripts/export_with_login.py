#!/usr/bin/env python3
"""用 nodriver 登录上游面板并导出「模型价格 + 成功率」JSON。

为什么不用 Playwright：Cloudflare Turnstile 会识别 Playwright/Puppeteer 的
CDP 特征（Runtime.enable 泄漏、自动化 flag、WebGL 指纹）。实测 Playwright
无论无头还是有头、无论怎么点，控件都停在「请验证您是真人」不动。
nodriver 走的是另一套附加方式，实测同样环境下能正常通过（约 20–30 秒）。

流程：
  1. 打开登录页
  2. 等 Turnstile 通过（必要时点一次复选框；坐标按控件容器启发式推算）
  3. 用 secrets.env 里的账号密码填表提交
  4. 用 CDP Network 事件截下应用自己发出的 Authorization 头
  5. 拿这个 token 调 /api/pricing 与 /api/perf-metrics/summary
  6. 合并写出 upstream-model-catalog/v1

用法：
    python3 scripts/export_with_login.py --host api.example.com \
        --login-url https://api.example.com/sign-in \
        --out out/models.json

凭据（自行写入 secrets.env，不要让 agent 代填）：
    UPSTREAM_USER_api_example_com=...
    UPSTREAM_PASS_api_example_com=...
"""

from __future__ import annotations

import argparse
import asyncio
import datetime
import json
import sys
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib import BROWSER_UA, load_secrets, proxy_url, urllib_opener  # noqa: E402


def lookup(secrets: dict[str, str], host: str, kind: str) -> str:
    dotted = host.replace(".", "_")
    bare = host.split(".")[0]
    for key in (f"UPSTREAM_{kind}_{dotted}", f"UPSTREAM_{kind}_{bare}"):
        value = secrets.get(key, "").strip()
        if value:
            return value
    sys.exit(f"secrets.env 缺少 UPSTREAM_{kind}_{dotted}（或 ..._{bare}）")


def chromium_path() -> str | None:
    for candidate in sorted(
        (Path.home() / ".cache" / "ms-playwright").glob("chromium-*/chrome-linux64/chrome")
    ):
        if candidate.exists():
            return str(candidate)
    return None


TURNSTILE_BOX_JS = """(() => {
  // Turnstile 的控件在 shadow DOM 里，querySelector 看不到。
  // 退而求其次：找表单里尺寸接近 300x65、位于提交按钮之后的容器，
  // 复选框在其中靠左约 18px。
  const form = document.querySelector('form');
  if (!form) return null;
  const submit = form.querySelector('button[type="submit"]');
  const anchor = submit ? submit.getBoundingClientRect().bottom : 0;
  let best = null;
  form.querySelectorAll('div').forEach((d) => {
    const r = d.getBoundingClientRect();
    if (r.width > 260 && r.width < 460 && r.height > 50 && r.height < 95 && r.top >= anchor - 10) {
      if (!best || r.top < best.top) best = {x: r.x, y: r.y, w: r.width, h: r.height, top: r.top};
    }
  });
  if (!best) return null;
  return {x: Math.round(best.x + 18), y: Math.round(best.y + best.h / 2)};
})()"""


async def login_and_capture(args, user: str, password: str, proxy: str | None) -> tuple[str, str]:
    import nodriver as uc
    from nodriver import cdp

    captured = {"token": "", "ua": ""}

    browser_args = ["--no-sandbox", "--window-size=1600,1000", "--disable-blink-features=AutomationControlled"]
    if proxy:
        # Chromium 只认 http(s):// 形式的代理地址；socks5 需写成 socks5://host:port
        browser_args.append(f"--proxy-server={proxy}")
        print(f"使用代理: {proxy.split('@')[-1]}", flush=True)

    browser = await uc.start(
        headless=False,
        browser_executable_path=chromium_path(),
        browser_args=browser_args,
        sandbox=False,
    )
    page = await browser.get(args.login_url)
    await page.sleep(8)

    # 记下这个浏览器真实的 UA：后续用同一个 token 直连接口时必须带上它。
    try:
        captured["ua"] = (await page.evaluate("navigator.userAgent")) or ""
    except Exception:  # noqa: BLE001
        captured["ua"] = ""

    await page.send(cdp.network.enable())

    def on_request(event):
        try:
            for key, value in (event.request.headers or {}).items():
                if key.lower() == "authorization" and value.startswith("Bearer ") and len(value) > 60:
                    captured["token"] = value[7:]
        except Exception:  # noqa: BLE001
            pass

    page.add_handler(cdp.network.RequestWillBeSent, on_request)

    # 等 Turnstile（不通过就点一次，再等）
    token_ok = False
    for _ in range(10):
        value = await page.evaluate(
            "(() => { const e=document.querySelector('input[name=\"cf-turnstile-response\"]'); return e? e.value : ''; })()"
        )
        if value:
            token_ok = True
            break
        point = await page.evaluate(TURNSTILE_BOX_JS)
        if point:
            try:
                await page.mouse_click(point["x"], point["y"])
            except Exception:  # noqa: BLE001
                pass
        await page.sleep(4)

    print(f"turnstile: {'ok' if token_ok else 'not detected (可能该站无验证)'}", flush=True)

    user_input = await page.select('input[name="username"], input[type="email"]', timeout=15)
    await user_input.send_keys(user)
    await page.sleep(0.3)
    password_input = await page.select('input[type="password"]', timeout=10)
    await password_input.send_keys(password)
    await page.sleep(0.3)

    # 条款勾选（未勾时提交按钮 disabled）
    try:
        consent = await page.select('span[role="checkbox"]', timeout=3)
        if consent:
            await consent.click()
    except Exception:  # noqa: BLE001
        pass
    await page.sleep(0.4)

    submit = await page.select('button[type="submit"]', timeout=10)
    await submit.click()
    await page.sleep(10)

    landed = await page.evaluate("location.href")
    print(f"after login: {landed}", flush=True)
    if "/sign-in" in landed or "/login" in landed:
        await page.save_screenshot("/tmp/login-failed.png")
        raise SystemExit("登录未成功：检查账号密码、或该站需要 2FA（截图见 /tmp/login-failed.png）")

    if not captured["token"]:
        # 触发一次数据请求，逼应用带上 Authorization
        try:
            await page.evaluate("history.pushState({}, '', '/pricing'); window.dispatchEvent(new PopStateEvent('popstate'));")
            await page.sleep(6)
        except Exception:  # noqa: BLE001
            pass

    browser.stop()
    if not captured["token"]:
        raise SystemExit("没有截到 access token：登录后应用未发出带鉴权的请求")
    # 会话绑定在 IP + UA 的哈希上（JWT 的 bnd 字段）。截到的 token 只在这个
    # 浏览器的 UA 下有效，所以后续直连接口必须沿用同一个 UA，否则会被判定
    # 为会话被窃取并吊销整个会话家族。
    ua = (captured["ua"] or BROWSER_UA).strip()
    return captured["token"], ua


def api(base: str, path: str, token: str, ua: str, proxy: str | None, timeout: int = 40) -> dict:
    request = urllib.request.Request(
        base.rstrip("/") + path,
        headers={"Authorization": f"Bearer {token}", "User-Agent": ua, "Accept": "application/json"},
    )
    # 代理必须和登录时一致：会话绑定同时校验出口 IP 与 UA。
    opener = urllib_opener(proxy)
    with opener.open(request, timeout=timeout) as response:
        return json.loads(response.read())


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True)
    parser.add_argument("--login-url", required=True)
    parser.add_argument("--out", default="out/upstream-models.json")
    parser.add_argument("--secrets")
    args = parser.parse_args()

    secrets = load_secrets(Path(args.secrets) if args.secrets else None)
    user = lookup(secrets, args.host, "USER")
    password = lookup(secrets, args.host, "PASS")
    proxy = proxy_url(secrets)

    token, ua = asyncio.run(login_and_capture(args, user, password, proxy))
    base = f"https://{args.host}"

    pricing = api(base, "/api/pricing", token, ua, proxy)
    rows = pricing.get("data") if isinstance(pricing.get("data"), list) else []
    if not rows:
        raise SystemExit("没有取到模型列表：该站可能改版或权限不足")

    try:
        summary = api(base, "/api/perf-metrics/summary?hours=24", token, ua, proxy)
    except Exception as exc:  # noqa: BLE001
        print(f"成功率接口不可用：{str(exc)[:120]}", flush=True)
        summary = {}

    perf = {m.get("model_name"): m for m in ((summary.get("data") or {}).get("models") or [])}
    models = []
    for row in rows:
        item = perf.get(row.get("model_name")) or {}
        models.append(
            {
                "model": row.get("model_name"),
                "quota_type": row.get("quota_type", 0),
                "model_ratio": row.get("model_ratio"),
                "completion_ratio": row.get("completion_ratio"),
                "model_price": row.get("model_price"),
                "enable_groups": row.get("enable_groups"),
                "success_rate": item.get("success_rate"),
                "avg_latency_ms": item.get("avg_latency_ms"),
            }
        )

    payload = {
        "schema": "upstream-model-catalog/v1",
        "source": base,
        "generated_at": datetime.datetime.now().astimezone().isoformat(timespec="seconds"),
        "window_hours": 24,
        "model_count": len(models),
        "models": models,
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    with_rate = sum(1 for m in models if m["success_rate"] is not None)
    print(f"导出 {len(models)} 个模型（{with_rate} 个带成功率）-> {out}")


if __name__ == "__main__":
    main()
