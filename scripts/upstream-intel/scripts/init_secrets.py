#!/usr/bin/env python3
"""交互式创建 secrets.env。值用 getpass 读入，不回显、不写日志。

agent 不要代替用户跑这个脚本——它需要真人输入密码。agent 只负责在文件
缺失时提示用户执行。
"""

from __future__ import annotations

import getpass
import os
import stat
from pathlib import Path

SECRETS = Path(
    os.environ.get("UPSTREAM_SECRETS_PATH")
    or (Path.home() / ".config" / "newapi-upstream-intel" / "secrets.env")
)

TEMPLATE = """# 上游价格同步的凭据。本文件必须是 0600，且不要提交到任何仓库。
NEWAPI_BASE={base}
NEWAPI_ADMIN_TOKEN={token}
# 需要鉴权的上游才填，键名是 UPSTREAM_KEY_<渠道id>
# UPSTREAM_KEY_6=sk-xxxx
# 需要登录态的上游才填
# UPSTREAM_USER_xcmapi=you@example.com
# UPSTREAM_PASS_xcmapi=...
"""


def main() -> None:
    base = input("new-api 站点地址（形如 https://api.example.com）: ").strip().rstrip("/")
    token = getpass.getpass("管理端访问令牌（输入不回显）: ").strip()
    if not base or not token:
        raise SystemExit("地址和令牌都不能为空")

    SECRETS.parent.mkdir(parents=True, exist_ok=True)
    SECRETS.write_text(TEMPLATE.format(base=base, token=token), encoding="utf-8")
    os.chmod(SECRETS, stat.S_IRUSR | stat.S_IWUSR)  # 0600

    print(f"已写入 {SECRETS}（0600）")
    print("如需补充上游密钥，请手动编辑该文件，加 UPSTREAM_KEY_<渠道id>=... 这一行。")


if __name__ == "__main__":
    main()
