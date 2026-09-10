# 路由单元去分组化迁移记录 (route-unit group collapse)

随 PR #494（issue #497）引入。本目录是该迁移的**存档记录**：它不是版本化 `.sql` 迁移，
而是 Go 启动迁移（原因见下），因此在此留档供运维审查。

## 1. 变更内容

`channel_model_routes`（路由单元 / 调度模型权重表）去掉用户分组维度：

| | 之前 | 之后 |
|---|---|---|
| 唯一键 | `(group, public_model_alias, channel_id, key_index, upstream_model)` | `(public_model_alias, channel_id, key_index, upstream_model)` |
| 行语义 | 每个分组一行（一个单元 × N 个可见分组 = N 行） | 一个调度单元一行 |
| 新列 | — | 无（纯删减 + 重建唯一索引） |

理由：分组只决定"用户能用哪些别名"（资格），从不参与权重与流量切分计算。
按分组展开使同一单元在每个分组下重复，管理界面无法区分、权重无法整体调整
（生产事故：6 行相同界面只调了 5 行，池内占比被压到 3/202），EWMA 样本也被分组切碎。

## 2. 实现位置与执行时序

- 实现：`apps/api/internal/common/dbx/migrate_route_unit_group.go` 的 `CollapseRouteUnitGroups()`
- 接入点：`apps/api/internal/dbinfra/open_db.go` 的 `migrateDB()`，**在 `AutoMigrate` 之前**调用
- 测试：`apps/api/internal/common/dbx/migrate_route_unit_group_test.go`

### 为什么不是 `db/migrations/` 里的版本化 `.sql`

1. **时序**：新唯一索引在多分组重复行存在时无法创建（PG/MySQL 拒绝对含重复数据的表建唯一索引），
   因此折叠必须先于 AutoMigrate；而版本化 `.sql` 执行器固定在 AutoMigrate 之后运行，时序上不可行。
2. **跨库**：折叠逻辑（布尔折叠、`group` 保留字引用）在 SQLite/MySQL/PostgreSQL 下方言各异，
   Go 实现一份代码覆盖三库，且有单元测试与 CI 兜底。
3. **可测试**：数据相关逻辑用 Go 写有 5 个单元测试 + 生产快照演练（239 行 → 65 单元）覆盖。

## 3. 折叠规则

同一 `(public_model_alias, channel_id, key_index, upstream_model)` 的分组兄弟行折叠为一行：

- 保留 `min(id)` 的行；
- `static_weight` 取兄弟行的**最大值**（分歧行本就无法在旧界面区分着调，分歧即事故；
  取 max 恢复种子默认 100 而不是采纳漏调的 2，生产那两处分歧 2..100 归一为 100）；
- `enabled` 取**或**（任一分组在服务即视为存活）；
- 其余兄弟行删除。

每次折叠与每个权重分歧行都会输出审计日志（`collapsed group-scoped route units: ...` /
`route unit ... had per-group weights 2..100; resolved to 100`），升级当天可在启动日志核对。

## 4. 幂等与安全

- **幂等**：以 `columnExists("channel_model_routes", "group")` 为门，首次运行后永久 no-op，重启安全；
- **事务**：删除 + 改写在同一事务内，失败整体回滚并**拒绝启动**（与其他迁移同一策略）；
- **影响面**：只触碰 `channel_model_routes` 一张表；
- 表不存在（全新安装）时为 no-op。

## 5. 等价参考 SQL（PostgreSQL，供人工审查）

```sql
-- 折叠（保留 min(id)，权重取 max，enabled 取或）
CREATE TABLE channel_model_routes_new AS
SELECT min(id) AS id, public_model_alias, channel_id, key_index, upstream_model,
       max(static_weight) AS static_weight,
       max(CASE WHEN enabled THEN 1 ELSE 0 END)::int AS enabled
FROM channel_model_routes
GROUP BY public_model_alias, channel_id, key_index, upstream_model;

DROP TABLE channel_model_routes;
ALTER TABLE channel_model_routes_new RENAME TO channel_model_routes;
-- 此后 AutoMigrate 创建新唯一索引：
-- CREATE UNIQUE INDEX idx_route_unit ON channel_model_routes
--   (public_model_alias, channel_id, key_index, upstream_model);
```

实际执行不使用以上 SQL（Go 实现按原表原地删除兄弟行并改写保留行，避免整表重建），
仅作行为对照。

## 6. 回滚与恢复

迁移是**单向**的（group 列删除后无法回退到旧版本二进制——旧代码查询 `group` 列会失败）。
升级生产前先做全库备份：

```bash
pg_dump -Fc -d newapi -f /backup/newapi-pre-degroup.dump   # 实测 12.5 万行 ≈ 0.6s
# 回滚 = 停服 → pg_restore → 回退旧版本二进制
```

可选的细粒度方案（未实现，留待评估）：迁移在折叠前把原表全量复制到
`channel_model_routes_pre_degroup` 备份表（本例 239 行，成本可忽略），
这样无需整库恢复即可单表还原。决定采用时在 Go 迁移里加建表语句即可，
本记录保留此方案备查。
