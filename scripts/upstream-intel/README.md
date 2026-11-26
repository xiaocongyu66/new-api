# 上游情报采集（upstream-intel）

从第三方上游站点采集**模型价格**与**模型健康度（成功率 / 延迟）**，产出 JSON 报告。
供「模型广场」图表与价格对比使用，也可以独立跑成定时任务。

**只读。** 这里没有任何修改价格、修改渠道路由的代码——采集结果交给人或后端决定。

## 两个作业

| 作业 | 脚本 | 建议频率 | 产出 |
|---|---|---|---|
| 价格表 | `scripts/collect_prices.py` → `scripts/diff_prices.py` | 24h | `out/prices.json`、差异报告 |
| 健康度 | `scripts/collect_health.py` → `scripts/report_health.py` | 1h | `out/health.json`、异常清单 |

两个作业共用同一套取数逻辑，区别只是 `specs/<host>.json` 里声明的取哪份数据。

## 取数阶梯

上游没有统一接口，实测三种形态，按代价从低到高：

| 形态 | 取法 | 例子 |
|---|---|---|
| 公开 JSON | 直接 GET + 声明式路径 | new-api `/api/pricing`、`/api/perf-metrics/summary` |
| sub2api 面板 | GET + 声明式路径（需 Bearer） | `/api/v1/model-plaza`、`/api/v1/usage` |
| 公开监控页 | GET 大 JSON | 自建 `/jk/status.json`（按「组」组织） |
| 服务端渲染 HTML | GET + CSS 选择器 | 老式面板 |
| 需要登录 / JS 渲染 | Playwright 渲染后取 DOM | 关掉定价模块的 SPA |

先用最便宜的，取不到才升级。任何一档解析出的数字都必须过数值校验：
**上游改版时宁可没数据，也不能把「暂无价格」当成 0 写进报告。**

## 凭据

凭据只放在仓库外的 `secrets.env`，权限必须是 `0600`：

```bash
python3 scripts/init_secrets.py        # 交互式创建，getpass，不回显
```

默认位置 `~/.config/newapi-upstream-intel/secrets.env`，可用
`UPSTREAM_SECRETS_PATH` 指向别处（例如 systemd 的凭据目录）。

变量命名规则：`UPSTREAM_<类型>_<域名点换下划线>`，例如

```
UPSTREAM_USER_xcmapi_org
UPSTREAM_PASS_xcmapi_org
UPSTREAM_KEY_6                  # 渠道 id 6 的中转密钥，用于 jk 自动定组
UPSTREAM_PROXY                  # 可选，限制出口 IP 的上游需要
NEWAPI_ADMIN_TOKEN              # 本站管理令牌（导出渠道清单用）
```

四条硬规则：

1. **不要把凭据写进仓库、日志或报告。** 脚本用 `lib.Redactor` 脱敏后再输出。
2. 只检查文件存在性与权限（`test -f`、`stat -c %a`），不打印内容。
3. 换密码后重跑 `init_secrets.py`，不要改代码。
4. `state/` 目录存 Playwright 登录态，等价于密码，已在 `.gitignore` 排除。

## 会话绑定（重要）

上游面板的会话绑定在 `hash(出口 IP + User-Agent)` 上（JWT 的 `bnd` 字段）。
登录时用哪个 UA，之后复用这份登录态就必须还是哪个 UA——**换 UA 会被判定会话被窃取，
服务端会吊销整个会话家族，把用户本人踢下线**。

所以：

- `lib.py` 里只有一个 `BROWSER_UA` 常量，不要在别处另写字面量。
- 登录成功会把实际 UA 写进 `state/ua.txt`，所有重放路径用 `lib.load_ua()` 读回来。
- 代理也必须一致：浏览器与直连接口要么都走、要么都不走。

## 目录

```
scripts/
  lib.py                    凭据加载、HTTP、脱敏、JSON 路径求值、p90
  collect_prices.py         价格采集（按 spec 走 api / scrape / browser）
  collect_health.py         健康度采集（jk / newapi 两级）
  diff_prices.py            价格差异报告
  report_health.py          健康度报告
  export_channels.py        导出渠道清单（不含密钥）
  probe_ladder.py           探测某个上游能用哪一档
  discover.py               自动发现可用接口
  browser_fetch.py          Playwright 适配器（读 stdin JSON，写 stdout JSON）
  login_upstream.py         登录并保存 storage_state
  export_with_login.py      登录后直接调接口导出（含成功率的模型清单）
  export_upstream_model_json.py  产出可导入的模型 JSON
  sync_prices.py            价格同步（只读比对，不改价）
specs/<host>.json           每个上游怎么取数
references/                 取数阶梯、浏览器回退、凭据、覆盖率说明
out/                        运行产物（已 gitignore）
```

## 跑起来

```bash
cd scripts/upstream-intel

# 1. 导出本站渠道清单（不含密钥）
python3 scripts/export_channels.py --out out/channels.json

# 2. 健康度
python3 scripts/collect_health.py --channels out/channels.json --out out/health.json
python3 scripts/report_health.py --in out/health.json

# 3. 价格
python3 scripts/collect_prices.py --channels out/channels.json --out out/prices.json
python3 scripts/diff_prices.py --in out/prices.json
```

两个采集器在**一条数据都没取到时以非 0 退出**，避免 systemd 把静默失败记成成功。

## 定时任务

```ini
# ~/.config/systemd/user/upstream-health.service
[Service]
Type=oneshot
WorkingDirectory=%h/homono/.worktree/.../scripts/upstream-intel
ExecStart=/usr/bin/python3 scripts/collect_health.py --channels out/channels.json --out out/health.json
ExecStartPost=/usr/bin/python3 scripts/report_health.py --in out/health.json

# ~/.config/systemd/user/upstream-health.timer
[Timer]
OnCalendar=hourly
Persistent=true
```

价格表把 `OnCalendar` 换成 `*-*-* 03:00:00` 即可。

## 已知缺口

- **xcmapi 类上游按「组」组织**，同一 host 下多个渠道分属不同组时，`specs/<host>.json`
  只能写一个 `jk_group_id`。多组场景需要按渠道覆盖（后端 `channel.settings.jk_group_id` 已支持）。
- **套餐商品名与上游模型名不是一一对应**（如 `[套餐]gemini-3.5-flash` 上游只有
  `gemini-3.5-flash-lite`），需要单独的映射表。
- **sub2api 的 access token 约 24h 过期**，脚本能自动用 refresh_token 或账号密码续期，
  但需要把账号密码配进 `secrets.env`。
