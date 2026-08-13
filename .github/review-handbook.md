# new-api 审查手册（Review Handbook）

Agent 和 维护者审查者共用此清单。Agent 在 PR diff 行上发现问题时，comment 必须以 `[Agent 🤖]:` 开头；无问题不留言。审查结论只发 `COMMENT`，不发 `APPROVE` / `REQUEST_CHANGES`，最终二审由维护者完成。

---

## 1. Go 后端

- [ ] Router → Controller → Service → Model 分层清楚；不把业务逻辑塞进 router / controller glue。
- [ ] 所有 JSON marshal/unmarshal 调用走 `common.Marshal` / `common.Unmarshal` / `common.DecodeJson` 等封装；业务代码不直接调用 `encoding/json`。
- [ ] 数据库变更同时兼容 SQLite、MySQL >= 5.7.8、PostgreSQL >= 9.6；优先 GORM，raw SQL 必须有方言分支与 fallback。
- [ ] `SELECT ... FOR UPDATE` 只通过 `model.lockForUpdate(tx)` 之类共享封装，不使用 GORM v1 `query_option`。
- [ ] 计费/额度路径不允许裸 `int(...)` / `math.Round(...)` 转 quota；必须走 `common.QuotaFromFloat` / `QuotaRound` / `QuotaFromDecimal` 及 checked 变体。
- [ ] 请求 DTO 的 optional scalar 字段使用 pointer + `omitempty`，显式 `0` / `false` 不被误删。
- [ ] `relaykit/` 不导入根模块；影响 relaykit 时必须能 `cd relaykit && GOWORK=off go build ./...`。

## 2. 前端 React / TypeScript

- [ ] 使用 Bun 工作流；改动 TS/TSX 后至少覆盖 `cd web && bun run typecheck`，构建相关改动覆盖 `bun run build`。
- [ ] 用户可见文案走 i18n；React 组件用 `useTranslation()` + `t('English key')`。
- [ ] 类型改动不引入 `any`；仅类型用途使用 `import type`。
- [ ] 性能 PR 必须有体积/加载/构建输出的前后证据；没有改善不能把 checklist 勾成完成。
- [ ] 懒加载必须有失败路径和可接受的 pending UI；不得因为 chunk 失败或慢网把整个内容永久空白。
- [ ] 动态 Tailwind class 不允许 `bg-${color}` 这类扫描器不可见写法；必须使用完整 literal map 或 safelist/source 配置。

## 3. 依赖与构建产物

- [ ] 删除依赖时同步 `web/package.json`、`web/bun.lock`，并确认源码无直接 import；transitive 残留要说明来源。
- [ ] 依赖 override 必须有版本选择理由，且不升级 React / provider / runtime 等非目标依赖。
- [ ] 构建产物去重必须检查 install tree 与 dist 产物；只看 lockfile 不够。
- [ ] CSS/JS 体积目标必须用同一 base、同一命令、同一 worktree 或明确可比的构建输出。

## 4. 安全

- [ ] 无硬编码密钥、真实配置、生产 token、私有 endpoint 或本机代理配置。
- [ ] `dangerouslySetInnerHTML` / markdown HTML 渲染必须经过 sanitizer，且链接属性不降低安全性。
- [ ] Cookie、JWT、OAuth、WebAuthn、rate limit、billing 权限路径不被前端绕过或后端降级。
- [ ] 日志与 PR/Issue/comment 不暴露密钥、真实配置或用户隐私。

## 5. 测试与验证

- [ ] Bug fix 需要能复现原问题并证明不再触发；功能/行为改动用现有测试或最小新增测试覆盖可观察契约。
- [ ] 前端改动至少跑受影响的 typecheck/build；UI 行为如果不能自动测，PR/Issue 明确保留人工目验项。
- [ ] 后端改动按影响范围跑 `go test` / `go build`；relaykit 改动单独跑 `GOWORK=off go build ./...`。
- [ ] 验证输出要能复现：命令、cwd、失败/通过结果、体积数字或 diff scope 都写清楚。

## 6. PR / Issue 卫生

- [ ] PR 标题为 Conventional Commits；Issue 标题自然语言，不写成 commit 风格。
- [ ] PR body 使用仓库模板；Issue/PR 正文英文 heading + 中文内容。
- [ ] 维护者要求不关闭时，只用 `Related #N`，不写 `Fixes #N` / closing keyword。
- [ ] Construction plan 的已完成 step 勾选；Issue Done when 只勾当前证据已满足的项。
- [ ] CRG 记录写 PR comment，不写 PR body。

---

## 语言要求

- PR 和 Issue 标题结构（What、Why、How to test、Issue、Checklist 等）可用英文，内容用中文。
- 同一文档不混用中英文表述；技术名词（包名、函数名、env 变量、文件路径）保持原文。

## Agent 审查流程

1. 读取 PR file changes / `git diff origin/main...HEAD`，只审当前 PR 相关文件。
2. 跑该 PR 对应的最小验证：前端 `bun run typecheck` / `bun run build`，后端 `go test` / `go build`，必要时加体积或静态扫描。
3. 跑 `code-review-graph update --base origin/main --brief`，CRG 结果只发 PR comment。
4. 按上述清单逐项检查 diff。
5. 只对发现的问题在对应文件行号留 `[Agent 🤖]:` comment；无问题不留。
6. 修复每条 Agent comment 后，在原 thread 回复 `[Agent 🤖]: 已修复...`，写明提交与验证。
7. 更新 Issue Done when 与 PR Construction plan / Delivery record 的 checkbox。
8. 提交 `COMMENT` 类型 summary review；维护者做最终二审。