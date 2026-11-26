"""公共支撑：凭据加载、HTTP、数值解析、脱敏。

凭据只从 secrets.env 读，绝不打印。任何异常信息在输出前都要过 redact()，
因为上游返回体里可能回显 token。
"""

from __future__ import annotations

import json
import math
import os
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

# 凭据默认放在仓库外，权限必须是 0600。部署时可以用
# UPSTREAM_SECRETS_PATH 指到别处（例如 systemd 的 EnvironmentFile 目录）。
SECRETS_PATH = Path(
    os.environ.get("UPSTREAM_SECRETS_PATH")
    or (Path.home() / ".config" / "newapi-upstream-intel" / "secrets.env")
)

# 所有出站请求统一用这一个 UA。
#
# 为什么必须是常量而不是各写一份：上游面板的会话绑定在 IP + UA 的哈希上
# （JWT 里的 bnd 字段）。登录时用哪个 UA，之后复用这份登录态就必须还是
# 哪个 UA——换了 UA 服务端会判定会话被窃取，返回 SESSION_BINDING_MISMATCH
# 并吊销整个会话家族，把用户本人踢下线。所以 UA 只在这里定义一次，
# 并由 state/ua.txt 记录「这份登录态是用哪个 UA 换来的」。
BROWSER_UA = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

# 这些键的值一旦出现在输出里就是事故，统一替换掉。
SECRET_KEYS = (
    "NEWAPI_ADMIN_TOKEN",
    "UPSTREAM_KEY_",
    "UPSTREAM_PASS_",
    "UPSTREAM_TOKEN_",
    "UPSTREAM_USER_",
)


def load_secrets(path: Path | None = None) -> dict[str, str]:
    """读取 secrets.env。缺失时给出可执行的修复指令，而不是静默降级。"""
    target = path or SECRETS_PATH
    if not target.exists():
        sys.exit(
            f"缺少凭据文件 {target}\n"
            f"请先运行: python3 scripts/init_secrets.py"
        )
    mode = oct(target.stat().st_mode & 0o777)
    if mode != "0o600":
        sys.exit(f"{target} 权限是 {mode}，必须是 0o600。执行: chmod 600 {target}")

    values: dict[str, str] = {}
    for raw in target.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        key = key.strip()
        # 允许值里带引号，但不去解释变量展开，避免意外执行。
        values[key] = value.strip().strip("'\"")
    return values


def cache_value(secrets: dict[str, str], key: str) -> str:
    """按变量名取一个必需值；只报「缺哪个名字」，不报别的值。"""
    value = secrets.get(key, "").strip()
    if not value:
        sys.exit(f"secrets.env 缺少 {key}")
    return value


class Redactor:
    """把已知的密钥值从任意文本里抹掉。"""

    def __init__(self, secrets: dict[str, str]):
        self._values = [
            v for k, v in secrets.items()
            if v and any(k.startswith(prefix) for prefix in SECRET_KEYS)
        ]

    def __call__(self, text: str) -> str:
        for value in self._values:
            if len(value) >= 6:
                text = text.replace(value, "***")
        # 兜底：形如 sk-xxxx / Bearer xxxx 的一律打码
        text = re.sub(r"(sk-[A-Za-z0-9_-]{4})[A-Za-z0-9_-]+", r"\1***", text)
        text = re.sub(r"(Bearer\s+)[A-Za-z0-9._-]{6,}", r"\1***", text)
        return text


def http_get_json(
    url: str,
    token: str | None,
    timeout: int = 20,
    redact: Redactor | None = None,
) -> dict:
    request = urllib.request.Request(url, method="GET")
    request.add_header("Accept", "application/json")
    # 不少上游在 Cloudflare 后面，会按客户端签名拦截默认的 Python UA
    # （表现为 403 + "Error 1010: Access denied"）。带上正常的 UA，
    # 否则明明是公开接口也会被当成爬虫挡掉。
    request.add_header(
        "User-Agent",
        BROWSER_UA,
    )
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    try:
        opener = urllib_opener(proxy_url())
        with opener.open(request, timeout=timeout) as response:
            body = response.read()
    except urllib.error.HTTPError as exc:
        detail = exc.read()[:300].decode("utf-8", "replace")
        text = f"HTTP {exc.code} for {url}: {detail}"
        raise RuntimeError(redact(text) if redact else text) from None
    except urllib.error.URLError as exc:
        text = f"连接失败 {url}: {exc.reason}"
        raise RuntimeError(redact(text) if redact else text) from None
    try:
        return json.loads(body)
    except json.JSONDecodeError:
        snippet = body[:200].decode("utf-8", "replace")
        text = f"{url} 返回的不是 JSON（可能是 SPA 的 HTML 兜底页）: {snippet}"
        raise RuntimeError(redact(text) if redact else text) from None


def parse_number(raw: str) -> float:
    """接受 '12.5'、'$0.038'、'1,234'、'54.17%' 这些写法。"""
    cleaned = str(raw).strip().lstrip("$").replace(",", "").rstrip("%")
    return float(cleaned)


def eval_path(root, path: str) -> list:
    """极小的 JSON 路径求值：data.items、[0]、[] 通配。

    和 Go 侧 internal/upstreamfetch 的语义保持一致，避免两边对同一份
    spec 给出不同结果。
    """
    current = [root]
    for segment in _split_path(path):
        nxt: list = []
        for node in current:
            if segment == "[]" or segment == "[*]":
                if isinstance(node, list):
                    nxt.extend(node)
                elif isinstance(node, dict):
                    nxt.extend(node[k] for k in sorted(node))
            elif segment.startswith("[") and segment.endswith("]"):
                index = int(segment[1:-1])
                if isinstance(node, list) and 0 <= index < len(node):
                    nxt.append(node[index])
            else:
                if isinstance(node, dict) and segment in node:
                    nxt.append(node[segment])
        current = nxt
        if not current:
            return []
    # 路径落在数组上时展开，调用方关心的是「有哪些项」
    expanded: list = []
    for node in current:
        if isinstance(node, list):
            expanded.extend(node)
        else:
            expanded.append(node)
    return expanded


def _split_path(path: str) -> list[str]:
    segments: list[str] = []
    token = ""
    index = 0
    while index < len(path):
        char = path[index]
        if char == ".":
            if token:
                segments.append(token)
                token = ""
            index += 1
        elif char == "[":
            if token:
                segments.append(token)
                token = ""
            end = path.index("]", index)
            segments.append(path[index : end + 1])
            index = end + 1
        else:
            token += char
            index += 1
    if token:
        segments.append(token)
    return segments


def scalar_text(value) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, float):
        # 避免 1.2e-06 这种写法被下游误读
        return f"{value:.12f}".rstrip("0").rstrip(".")
    return str(value)


def write_json(path: Path, payload) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


def ua_sidecar(state_file: str | Path) -> Path:
    """登录态旁边记录「它是用哪个 UA 换来的」。"""
    return Path(state_file).with_name("ua.txt")


def load_ua(state_file: str | Path) -> str:
    """取这份登录态签发时用的 UA。

    复用登录态必须沿用同一个 UA：会话绑定在 IP + UA 的哈希上，
    换 UA 会被服务端判定为会话被窃取并吊销整个会话家族。
    """
    sidecar = ua_sidecar(state_file)
    if sidecar.exists():
        value = sidecar.read_text(encoding="utf-8").strip()
        if value:
            return value
    return BROWSER_UA


def save_ua(state_file: str | Path, user_agent: str) -> None:
    """登录成功后立刻记下实际使用的 UA。"""
    sidecar = ua_sidecar(state_file)
    sidecar.parent.mkdir(parents=True, exist_ok=True)
    sidecar.write_text((user_agent or BROWSER_UA).strip() + "\n", encoding="utf-8")


def token_from_state(
    state_file: str | Path,
    base: str,
    redact=None,
    user_agent: str | None = None,
    proxy: str | None = None,
) -> str | None:
    """用 Playwright 载入登录态，换出一个 access token。

    new-api 的接口要 Authorization: Bearer <access_token>，而 storage_state
    里通常只有 refresh cookie。所以先在页面上下文里调一次刷新接口，
    让服务端把 cookie 换成新的 access token。

    这比"把 cookie 直接塞进 requests"可靠：cookie 只对浏览器语义有效，
    API 鉴权看的是 Bearer。

    UA 与代理都必须和登录时一致，否则会触发会话绑定校验。
    """
    path = Path(state_file)
    if not path.exists():
        return None
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        return None

    ua = user_agent or load_ua(path)
    egress = proxy if proxy is not None else proxy_url()

    chrome = None
    for candidate in sorted((Path.home() / ".cache" / "ms-playwright").glob("chromium-*/chrome-linux64/chrome")):
        if candidate.exists():
            chrome = str(candidate)
            break

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True, executable_path=chrome, args=["--no-sandbox"])
        context_kwargs = {"storage_state": str(path), "user_agent": ua}
        if egress:
            context_kwargs["proxy"] = {"server": egress}
        context = browser.new_context(**context_kwargs)
        page = context.new_page()
        try:
            page.goto(base.rstrip("/") + "/", wait_until="domcontentloaded", timeout=45000)
            page.wait_for_timeout(1500)
            result = page.evaluate("""async () => {
              const tryPaths = ['/api/user/auth/refresh', '/api/user/self'];
              for (const url of tryPaths) {
                try {
                  const r = await fetch(url, {method: url.includes('refresh') ? 'POST' : 'GET',
                                               headers: {'Content-Type': 'application/json'},
                                               credentials: 'include'});
                  const t = await r.text();
                  if (!t) continue;
                  const j = JSON.parse(t);
                  const tok = j?.data?.access_token;
                  if (tok) return {token: tok, via: url};
                } catch (e) {}
              }
              for (const k of ['token','access_token','auth_token']) {
                const v = localStorage.getItem(k);
                if (v && v.length > 20) return {token: v, via: 'localStorage:'+k};
              }
              return {token: null};
            }""")
            token = (result or {}).get("token")
            if token:
                print(f"已从登录态换出 access token（via {result.get('via')}）")
            else:
                print(redact("登录态里没有可用的 access token；可能需要重新登录") if redact else "no token")
            return token
        except Exception as exc:  # noqa: BLE001
            print(redact(f"换 token 失败: {str(exc)[:160]}") if redact else str(exc)[:160])
            return None
        finally:
            context.close()
            browser.close()

# ---- 代理支持 ----
# 有些上游限制出口 IP（例如只允许境外访问）。这时在 secrets.env 里加一行：
#   UPSTREAM_PROXY=http://127.0.0.1:7890
# 或  UPSTREAM_PROXY=socks5://127.0.0.1:1080
# 所有 HTTP 取数与浏览器启动都会走它。它和 token 是两回事：
# 代理解决「连不上」，token/账号解决「进不去」。

def proxy_url(secrets: dict[str, str] | None = None) -> str | None:
    values = secrets if secrets is not None else {}
    for key in ("UPSTREAM_PROXY", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"):
        value = (values.get(key) or os.environ.get(key) or "").strip()
        if value:
            return value
    return None


def urllib_opener(proxy: str | None = None):
    """按需构造带代理的 opener；无代理时返回默认 opener（支持系统代理）。"""
    if not proxy:
        return urllib.request.build_opener()
    handler = urllib.request.ProxyHandler({"http": proxy, "https": proxy})
    return urllib.request.build_opener(handler)

def percentile(values: list[float], q: float) -> float | None:
    """最近秩法取分位数。

    不要用 int(n*q) 当下标：n=10、q=0.9 时它等于 9，直接取到最大值，
    把 p90 变成 max，长尾被系统性高估。
    """
    if not values:
        return None
    ordered = sorted(values)
    if len(ordered) == 1:
        return ordered[0]
    rank = max(1, math.ceil(q * len(ordered)))
    return ordered[min(rank, len(ordered)) - 1]
