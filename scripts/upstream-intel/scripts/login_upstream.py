#!/usr/bin/env python3
"""用上游面板的账号密码登录，产出可复用的登录态文件。

为什么需要它：第三方上游不会给你访问令牌，你只有账号密码。而 agent 不该
知道密码。所以把「读密码 + 驱动登录」整个放进这个脚本：

    agent   →  调用本脚本  →  只拿到 state/<host>.json（cookie 快照）
    密码    →  只在脚本进程内存里出现一次，不打印、不落盘、不进上下文

用法：

    # 常规登录（无 2FA）
    python3 scripts/login_upstream.py --host xcmapi.org --login-url https://xcmapi.org/sign-in

    # 有验证码/短信/2FA：开有头浏览器，人工完成后回车
    python3 scripts/login_upstream.py --host xcmapi.org --login-url https://xcmapi.org/sign-in --manual

    # 检查现有登录态还能不能用
    python3 scripts/login_upstream.py --host xcmapi.org --check https://xcmapi.org/console
"""

from __future__ import annotations

import argparse
import os
import stat
import sys
from pathlib import Path

# 用完整 Chromium 而不是 headless shell：后者更容易被指纹识别，
# 部分站点的登录页在 shell 下渲染不全。找不到就退回 Playwright 默认。
CHROMIUM_CANDIDATES = sorted(
    (Path.home() / ".cache" / "ms-playwright").glob("chromium-*/chrome-linux64/chrome")
)


def chromium_path() -> str | None:
    for candidate in CHROMIUM_CANDIDATES:
        if candidate.exists():
            return str(candidate)
    return None

sys.path.insert(0, str(Path(__file__).parent))
from lib import BROWSER_UA, Redactor, load_secrets, load_ua, proxy_url, save_ua  # noqa: E402

CONFIG_ROOT = Path.home() / ".config"


def config_dir() -> Path:
    """按技能名定位配置目录，避免两个技能互相覆盖登录态。"""
    name = Path(__file__).resolve().parent.parent.name
    return CONFIG_ROOT / name


def state_path(host: str) -> Path:
    return config_dir() / "state" / f"{host}.json"


def secret_key(host: str, kind: str) -> list[str]:
    """同一台主机允许两种写法：点换下划线，或只用主域名。"""
    dotted = host.replace(".", "_")
    bare = host.split(".")[0]
    return [f"UPSTREAM_{kind}_{dotted}", f"UPSTREAM_{kind}_{bare}"]


def lookup(secrets: dict[str, str], host: str, kind: str) -> str:
    for key in secret_key(host, kind):
        value = secrets.get(key, "").strip()
        if value:
            return value
    sys.exit(
        f"secrets.env 缺少 {' 或 '.join(secret_key(host, kind))}\n"
        f"请自行编辑该文件加入这一行（不要让 agent 代填）：\n"
        f"  {secret_key(host, kind)[0]}=..."
    )


def has_display() -> bool:
    return bool(os.environ.get("DISPLAY") or os.environ.get("WAYLAND_DISPLAY"))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True, help="上游域名，如 xcmapi.org")
    parser.add_argument("--login-url", help="登录页地址")
    parser.add_argument("--check", help="只检查现有登录态：访问这个 URL 看是否被踢回登录页")
    parser.add_argument("--manual", action="store_true", help="有头浏览器，人工完成验证码/2FA")
    parser.add_argument("--secrets", help="secrets.env 路径（默认按技能名定位）")
    args = parser.parse_args()

    host = args.host.strip()
    target_state = state_path(host)
    secrets = load_secrets(Path(args.secrets) if args.secrets else None)
    redact = Redactor(secrets)

    if args.check:
        rc = check_state(target_state, args.check, redact)
        raise SystemExit(rc)

    if not args.login_url:
        sys.exit("登录需要 --login-url")

    user = lookup(secrets, host, "USER")
    password = lookup(secrets, host, "PASS")
    login_upstream(host, args.login_url, user, password, target_state, redact, args.manual)


def check_state(state_file: Path, url: str, redact: Redactor) -> int:
    """访问一个登录后才能看的页面，判断 cookie 是否还有效。

    必须沿用登录时的 UA 与代理：会话绑定在 IP + UA 的哈希上，
    用不同的 UA 去「检查」会被判定为会话被窃取并吊销会话家族——
    也就是这条检查命令本身会把用户踢下线。
    """
    if not state_file.exists():
        print(f"没有登录态文件 {state_file}，先跑一次登录")
        return 2
    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True, args=["--no-sandbox"], executable_path=chromium_path())
        context_kwargs = {"storage_state": str(state_file), "user_agent": load_ua(state_file)}
        proxy = proxy_url(load_secrets())
        if proxy:
            context_kwargs["proxy"] = {"server": proxy}
        context = browser.new_context(**context_kwargs)
        page = context.new_page()
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=45000)
            page.wait_for_timeout(2500)
            landed = page.url
            if any(token in landed for token in ("/login", "/sign-in", "/auth")):
                print(f"登录态已失效（被重定向到 {landed}）")
                return 1
            print(f"登录态有效，停在 {landed}")
            return 0
        except Exception as exc:  # noqa: BLE001
            print(redact(f"检查失败: {str(exc)[:200]}"))
            return 1
        finally:
            context.close()
            browser.close()


def login_upstream(
    host: str,
    login_url: str,
    user: str,
    password: str,
    state_file: Path,
    redact: Redactor,
    manual: bool,
) -> None:
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        sys.exit("playwright 未安装：pip install playwright && playwright install chromium")

    headless = not manual
    if manual and not has_display():
        sys.exit("--manual 需要图形界面（DISPLAY/WAYLAND_DISPLAY），当前没有")

    with sync_playwright() as p:
        launch_kwargs = {"headless": headless, "executable_path": chromium_path(),
                         "args": ["--no-sandbox", "--disable-blink-features=AutomationControlled"]}
        proxy = proxy_url(load_secrets())
        if proxy:
            launch_kwargs["proxy"] = {"server": proxy}
        browser = p.chromium.launch(**launch_kwargs)
        context = browser.new_context(
            user_agent=BROWSER_UA,
            viewport={"width": 1440, "height": 900},
            locale="zh-CN",
        )
        page = context.new_page()
        try:
            page.goto(login_url, wait_until="networkidle", timeout=60000)
            page.wait_for_timeout(1500)

            if manual:
                print("请在打开的窗口里完成登录（含验证码/2FA），完成后回到这里按回车…")
                input()
            else:
                fill_login_form(page, user, password, redact)
                submit_login(page)

            page.wait_for_timeout(4000)
            if any(token in page.url for token in ("/login", "/sign-in", "/auth")):
                # 停在登录页通常意味着凭证被拒。不要在这里回显任何提交内容。
                print(redact(f"仍停留在登录页（{page.url}）。常见原因：凭证错误、账号被封、需要验证码或 2FA"))
                print("建议：改用 --manual 人工确认一次；或核对 secrets.env 里的账号名")
                raise SystemExit(1)

            state_file.parent.mkdir(parents=True, exist_ok=True)
            context.storage_state(path=str(state_file))
            os.chmod(state_file, stat.S_IRUSR | stat.S_IWUSR)  # 0600
            # 记下这份登录态是用哪个 UA 换来的，之后复用必须沿用同一个。
            save_ua(state_file, BROWSER_UA)
            print(f"登录成功，已保存登录态到 {state_file}（0600）")
            print("提示：登录态等价于密码，不要提交到仓库、不要贴给 agent。")
        finally:
            context.close()
            browser.close()


def fill_login_form(page, user: str, password: str, redact: Redactor) -> None:
    """尽量通用地填登录表单：用户名、密码、必要的条款勾选。"""
    user_selector = "input[name='username'], input[type='email'], input[autocomplete='username'], input[name='email']"
    pass_selector = "input[type='password']"

    try:
        page.wait_for_selector(user_selector, timeout=20000)
    except Exception:  # noqa: BLE001
        sys.exit(redact("找不到用户名输入框，页面结构可能变了；用 --manual 人工登录"))

    page.fill(user_selector, user)
    page.fill(pass_selector, password)

    # 条款勾选：未勾时提交按钮通常是 disabled。很多 UI 库用 span[role=checkbox] 包隐藏 input。
    consent = page.query_selector("span[role='checkbox'], input[type='checkbox']")
    if consent:
        try:
            consent.click()
        except Exception:  # noqa: BLE001
            pass


def submit_login(page) -> None:
    submit = page.query_selector("button[type='submit']")
    if submit and submit.is_disabled():
        # 勾选后仍禁用，说明还有前置条件没满足
        pass
    if submit:
        submit.click()
    else:
        page.press("input[type='password']", "Enter")


if __name__ == "__main__":
    main()
