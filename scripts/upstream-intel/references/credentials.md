# 凭据怎么放，agent 才看不到

目标：**agent 全程不知道密钥内容**，只让脚本在运行时读。

## 一句话

密钥放在一个 0600 的文件里，由脚本 `open()` 读取；agent 只看「文件存在吗、
权限对不对、变量名有没有」，**从不读值**。

## 具体要填什么

文件路径（两个技能各自独立）：

```
~/.config/newapi-upstream-intel/secrets.env
~/.config/newapi-upstream-intel/secrets.env
```

权限：

```bash
chmod 600 ~/.config/newapi-upstream-intel/secrets.env
```

内容（等号两边不要加引号，也不要加 `export`）：

```dotenv
# new-api 站点地址
NEWAPI_BASE=https://your-site.example

# 管理端访问令牌（不是登录密码）
NEWAPI_ADMIN_TOKEN=your-token-here

# 只有需要鉴权的上游才填，键名是 UPSTREAM_KEY_<渠道id>
UPSTREAM_KEY_6=sk-xxxxxxxx

# 只有走 sub2api 用量接口的上游才填
UPSTREAM_TOKEN_6=panel-access-token

# 只有 browser 档需要登录态时才会用到（通常不用填）
UPSTREAM_USER_somehost=you@example.com
UPSTREAM_PASS_somehost=...
```

**推荐只填前两个。** 后三个是可选：大多数价格接口是公开的，不需要任何上游密钥。

## 令牌怎么来（不要用登录密码）

用**访问令牌**，不是账号密码：

1. 登录面板 → 个人设置 / 账户 → 「访问令牌」
2. 新建一个，名字写清楚用途（如 `price-sync-cron`），权限给只读或管理
3. 复制出来的字符串填进 `NEWAPI_ADMIN_TOKEN`

好处：

- 可以单独吊销，不影响你本人的登录
- 有独立有效期与权限范围
- 泄露了只废这一个令牌

如果面板没有「访问令牌」入口，再退而用管理员账号的登录态，但**不要把密码
交给 agent**——见下面的「解绑」。

## 解绑：让已经接触过凭据的 agent 再也用不上

按顺序做，做完前面那条路就彻底断了：

| 步骤 | 动作 | 效果 |
|---|---|---|
| 1 | **改登录密码** | 旧的会话/密码立即失效 |
| 2 | 面板里「退出所有设备」/ 吊销会话 | 已签发的 access token 失效 |
| 3 | 删除刚才那个访问令牌，重新建一个 | 旧的 PAT 作废 |
| 4 | 把新令牌写进 `secrets.env`，`chmod 600` | 只有脚本能读 |
| 5 | `rm` 掉任何临时文件（/tmp 里的 creds、state） | 清理落地痕迹 |

第 1、3 步是关键：**改密码 + 换令牌**之后，之前泄露的那串东西就是废纸。

## 为什么 agent 看不到

- 技能里的脚本用 `load_secrets()` 读文件，agent 只调用脚本，不读文件内容；
- SKILL.md 明确要求：**不要 `cat`/`echo`/`env` 输出它**，只 `test -f` 和 `stat -c %a`；
- 所有异常信息过 `Redactor`，`sk-...`、`Bearer ...` 一律打码；
- 定时任务是 systemd 直接跑脚本，**agent 不参与例行执行**，上下文里根本没有凭据。

最后一条最重要：agent 只在脚本失败或接入新上游时介入，那时候它拿到的
也只是「哪个渠道失败了、失败原因是什么」，不含任何密钥。

## systemd 场景（更严）

如果想让文件连用户目录都不落，用 systemd 的凭据注入：

```ini
[Service]
LoadCredential=secrets.env:/etc/newapi-upstream-intel/secrets.env
# 脚本从 $CREDENTIALS_DIRECTORY/secrets.env 读
```

文件放在 `/etc/`、root 600，服务以专用用户运行。这样即使同机的其他进程
也读不到。

## 检查清单

```bash
test -f ~/.config/newapi-upstream-intel/secrets.env && echo OK
stat -c '%a' ~/.config/newapi-upstream-intel/secrets.env   # 必须是 600
grep -c '^NEWAPI_ADMIN_TOKEN=' ~/.config/newapi-upstream-intel/secrets.env  # 1
```

第三条只数行数，不打印值。

---

# 第三方上游面板：没有令牌可用

这是和「自己的 new-api」完全不同的问题。上游是别人的站，你是客户，
人家不会给你发访问令牌——**你手上只有账号密码**。

## 那怎么让 agent 不知道密码

把「读密码 + 登录」整个塞进一个脚本，agent 只拿登录态的产物：

```
secrets.env (0600)  ──读──▶  login_upstream.py  ──写──▶  state/<host>.json (0600)
                                      ▲                          │
                                    agent 只调用                agent 只加载
                                  （传 host/URL，不碰密码）      （cookie 快照）
```

### 要填什么

```dotenv
# 主机名里的点换成下划线
UPSTREAM_USER_xcmapi_org=你的账号
UPSTREAM_PASS_xcmapi_org=你的密码
```

也支持只写主域名：`UPSTREAM_USER_xcmapi` / `UPSTREAM_PASS_xcmapi`。

### 怎么用

```bash
# 登录一次，产出登录态
python3 scripts/login_upstream.py --host xcmapi.org --login-url https://xcmapi.org/sign-in

# 有验证码 / 短信 / 2FA：开有头浏览器，人工完成后回车
python3 scripts/login_upstream.py --host xcmapi.org --login-url https://xcmapi.org/sign-in --manual

# 检查登录态还有效吗
python3 scripts/login_upstream.py --host xcmapi.org --check https://xcmapi.org/console
```

`browser_fetch.py` 的 job 里传 `"storage_state": "~/.config/.../state/xcmapi.org.json"`
就能带着登录态取数。

### 密码换了怎么办

1. 编辑 `secrets.env` 改那两行（**你自己改，不要让 agent 代填**）
2. `rm ~/.config/<skill>/state/<host>.json`
3. 重跑一次 `login_upstream.py`

### 有 2FA / 图形验证码怎么办

用 `--manual`：脚本开一个有头浏览器，你手动完成，回车后它保存登录态。
之后每次采集都复用这个 cookie，直到过期。**密码始终不进 agent 上下文。**

### 登录态会过期

cookie 有寿命，且有些站会在 IP/UA 变化时失效。脚本 `--check` 可以判断；
定时任务发现被踢回登录页时，重新跑一次 `login_upstream.py` 即可
（如果密码没变，这一步可以全自动）。

### 安全边界

| 风险 | 缓解 |
|---|---|
| 密码出现在 agent 上下文 | 只存在 secrets.env，脚本读，agent 不读 |
| 密码出现在日志 | 脚本不打印；异常过 `Redactor`；提示文案里只有变量名 |
| 登录态泄露 | `state/*.json` 0600，加进 `.gitignore`，等价于密码对待 |
| 同机其他进程读取 | 用独立系统用户跑，或 systemd `LoadCredential` 把文件放 `/etc` |
| 上游封号 | 别高频重登；登录失败就停，不要重试轰炸（会触发风控） |

## 代理（出口 IP 受限时）

有些上游只允许特定地区的 IP 访问（例如境外站封国内 IP）。这时需要代理。
**代理和凭据是两件不同的事**：

| 问题 | 解决手段 |
|---|---|
| 连不上（403 / Cloudflare 拦截 / 超时） | `UPSTREAM_PROXY` |
| 连上了但进不去（401 / 登录页） | `UPSTREAM_TOKEN_*` 或 `UPSTREAM_USER_*/PASS_*` |

### 填法

```dotenv
# 只要有这一行，HTTP 取数和浏览器都会走它
UPSTREAM_PROXY=http://127.0.0.1:7890
# 或
UPSTREAM_PROXY=socks5://127.0.0.1:1080
```

然后照常跑：

```bash
python3 scripts/probe_ladder.py https://<host>
python3 scripts/export_with_login.py --host <host> --login-url https://<host>/login --out out/x.json
```

脚本会打印 `使用代理: host:port`（不含账号密码）。

### 支持的写法

- `http://host:port`
- `socks5://host:port`（Playwright/Chromium 原生支持）
- `http://user:pass@host:port`（带认证）

### 注意

- 代理只影响出站请求，不影响 `secrets.env` 的读取。
- 如果代理进程在你本机、而脚本跑在另一台机器上，把 `127.0.0.1` 换成那台机器的可达地址，并确认端口只对可信网络开放。
- SOCKS5 在 `urllib` 下需要用 `socks5h://` 才能让 DNS 也走代理解析；Chromium 无此限制。
