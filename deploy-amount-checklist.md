# PR #513 部署后数额核对清单 — 生产 nailao (154.12.51.245)

部署前基线采集时间 2026-09-16。容器 `new-api-staging`，当前镜像
`ghcr.io/xiaocongyu66/new-api:sha-1b39ecdd2ce7`，PostgreSQL `newapi-postgres`
（库名 `newapi`，角色 `postgres`）。

本 PR **不改动任何已存储的数额**（见"设计说明"）。下列检查的作用是确认
升级后展示侧换算确实生效、新列已建、配置回环正常。

## 0. 部署前快照（先存档，再升级）

```bash
ssh nailao 'docker exec newapi-postgres pg_dump -U postgres -d newapi \
  --table=users --table=options --table=subscription_plans \
  --table=user_subscriptions --table=redemptions > /tmp/pre-pr513.sql'
```

## 1. 数据库层（升级后立即查）

- [ ] `users.aff_spore_history` 列已创建
      `SELECT column_name, data_type, column_default FROM information_schema.columns
       WHERE table_name='users' AND column_name='aff_spore_history'`
      预期：`bigint`，`not null`，默认 `0`。由 AutoMigrate 自动加（`migrate_identity.go`
      已注册 `User{}`），**不需要手写迁移**。
- [ ] 该列对所有老用户为 0（这是**新计数器**，不是对历史数据的换算）
      `SELECT count(*) FILTER (WHERE aff_spore_history <> 0) FROM users` → 预期 `0`
- [ ] `spore` 余额列**未变**：`SELECT sum(spore) FROM users` → 仍是 `7469`（十分位）
- [ ] `options` 表的金额键**未变**（仍是原始 quota）：
      `QuotaForInviter=10000000`、`QuotaForInvitee=5000000`、
      `QuotaForNewUser=250000`、`PreConsumedQuota=500`、`SporeInviterReward=0.1`

## 2. /api/status 展示侧（匿名可访问）

- [ ] `inviter_reward_display` = `20`（= 10000000 / 500000，CUSTOM 汇率默认 1.0）
- [ ] `invitee_reward_display` = `10`（= 5000000 / 500000）
- [ ] **不再出现** `quota_per_unit`、`quota_for_inviter`、`quota_for_invitee`
      （这是本 PR 的泄漏修复点，匿名访客此前能看到内部记账粒度）
- [ ] `spore_inviter_reward` = `0.1`（整数菌种，非十分位）
- [ ] `amount_unit` = `custom`，符号 `🧀`，`amount_name` = `🍄`

## 3. 用户展示侧（登录后）

抽样核对（基线值 → 升级后应显示）：

| 用户 | 字段 | 库内原始值 | 应显示 |
|---|---|---|---|
| 神 (id 1062) | 剩余额度 | 4453732935 | `🧀 8907.47` |
| 儒意3333 (id 259) | 剩余额度 | 4048482500 | `🧀 8096.97` |
| 儒意3333 | 邀请收益 aff_quota | 410000000 | `🧀 820` |
| 儒意3333 | 菌种余额 spore | 830（十分位） | `🍄 83.0` |
| 琪琪酱 (id 773) | 邀请累计 aff_history | 500000000 | `🧀 1000` |

- [ ] `/api/user/self` 返回 `quota_display`/`used_quota_display`/`aff_quota_display`/
      `aff_history_quota_display`/`aff_spore_history_display`，且与上表一致
- [ ] 前端钱包卡片不再出现 `4453732935` 这类原始整数
- [ ] **菌种"总收入"从 0 开始**（新计数器，见"设计说明"第 3 条）——
      老用户的菌种"待确认/总收入"在升级后先是 0，之后新邀请才累加。
      余额 `83.0` 不受影响。**这是预期行为，不是 bug**。

## 4. 订阅 / 兑换码 / 渠道

- [ ] 套餐卡 `total_amount_display`：套餐1 `25000000` → `🧀 50`；套餐3 `2500000` → `🧀 5`
- [ ] 用户订阅 `amount_total_display`/`amount_used_display` 已下发
      （总量 `800605000` → 全站 `🧀 1601.21`）
- [ ] 兑换码列表走 `quota_display`（全站 `575000000` → `🧀 1150`）
- [ ] 渠道标签聚合行：分组后"已用"是子渠道之和，不是首个子渠道的值
- [ ] 日志统计 badge `quota_display`（近 24h `250241000` → `🧀 500.48`）

## 5. 配置回环（管理员后台）

- [ ] 邀请设置页"邀请者奖励"输入框种子值为 `20`（不是 10000000）
- [ ] 改成 `30` 保存 → 库内 `QuotaForInviter` 变为 `15000000`，页面刷新仍显示 `30`
- [ ] **合规门**：`payment_setting.compliance_confirmed` 当前为 `true`；
      若改为 false，`QuotaForInviter` 与 `QuotaForInviter_display` 两种键
      **都**应被拒绝（本 PR 修的绕过：此前 display 键能绕过合规确认）

## 6. 回滚

代码层回滚 = 切回 `sha-1b39ecdd2ce7` 镜像。`aff_spore_history` 列留下无害
（老代码不读它）。**没有不可逆的数据迁移**。

---

## 设计说明：为什么旧数值不会偏差（回答问题 2）

**是的，数额都在数据库里，而且本 PR 一个存储值都没改。**

1. **DB 存的是原始 quota（整数），展示换算发生在 API 出口，不在入库。**
   `users.quota`、`options.QuotaForInviter`、`subscription_plans.total_amount`
   全是原始内部单位（500000 = 1 展示单位）。本 PR 加的 `QuotaToDisplayAmount`
   在 `AfterFind` hook / 响应组装时**读取时换算**，写库路径完全没变。
   所以老数据、新数据用同一条规则换算，不存在"老数据需要迁移"的问题。

2. **为什么这能保证不偏差**：换算是 `stored / QuotaPerUnit × rate` 的纯函数。
   只要 `QuotaPerUnit`（500000）和 `custom_currency_exchange_rate` 不变，
   展示值就是确定的。生产 `quota_display_type=CUSTOM`、
   `custom_currency_exchange_rate` **不在 options 表里 → 走默认 1.0**。
   ⚠️ **升级前请确认这个默认 1.0 是你想要的**；若当初想配别的汇率，
   现在去设置页配，所有展示值会统一变化（不是偏差，是统一调整）。

3. **唯一的新列 `users.aff_spore_history` 不是换算，是新计数器。**
   以前菌种奖励"发放即入 spore 余额，从不记账"，所以没有历史可回填。
   它从 0 开始累计，只影响推荐卡片的菌种"总收入"展示。菌种**余额** (`spore` 列)
   是另一回事，完全没动。

4. **前端以前显示大数，是因为前端拿到了原始 quota 自己除（或没除）。**
   现在前端只收 `*_display` 兄弟字段，`formatQuota` 不再做任何乘除。
   第 4 轮审查还把 `*_display ?? 原始值` 的兜底去掉了——后端保证每个读路径
   都填（`AfterFind`/`fillLogQuotaDisplay`/`fillQuotaDisplay`），兜底只会
   把未换算整数当货币渲染，正是要消除的泄漏。
