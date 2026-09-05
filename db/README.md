# 数据库迁移文件说明 (Database Migrations)

本目录用于存放 `new-api` 的数据库结构迁移脚本与历史归档。

---

## 1. 目录结构

```text
db/
├── migrations/ -> ../apps/api/internal/dbinfra/migrations/   # 核心版本化迁移脚本
│   ├── 001_drop_unused_log_indexes.sql                      # 删除 6 个零扫描冗余索引
│   └── 002_logs_timescale_hypertable.sql                    # 将 logs 转换为 TimescaleDB 超表
├── archive/                                                 # 历史旧版本手动补丁归档
│   ├── migration_v0.2-v0.3.sql
│   └── migration_v0.3-v0.4.sql
└── README.md
```

---

## 2. 为什么实际文件位于 `apps/api/internal/dbinfra/migrations/`？

Go 语言标准库的 `//go:embed` 指令**严格禁止引用包含 `..` 的跨模块/跨包路径**。
为了确保编译出的单个独立可执行文件（如 Docker 镜像中的 `/new-api`）能够直接包含所有的 SQL 迁移脚本，实际的 SQL 文件必须与嵌入代码同位于 `apps/api/internal/dbinfra/migrations/` 目录下。

根目录下的 `db/migrations` 通过符号链接指向该真实路径，便于开发者和运维人员直接查阅、管理与检索。

---

## 3. 迁移执行机制

- **自动运行**：`new-api` 实例每次启动时，在 GORM `AutoMigrate` 之后会自动运行内置的迁移执行器；
- **顺序执行**：按文件名（`001_...`, `002_...`）顺序在独立事务中执行；
- **记账防重**：执行成功的版本记录于 `schema_migrations` 表，重复启动自动跳过；
- **跨数据库隔离**：SQL 文件头部可通过 `-- applies-to: postgres,mysql` 指定适用的数据库类型，不适用的数据库类型会自动安全跳过。
