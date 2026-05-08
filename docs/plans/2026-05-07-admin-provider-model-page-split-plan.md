# Admin Provider Model Page Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将当前“后台模型”页拆分为“后台模型（只读）”与“新建模型（独立左侧导航）”两个 admin 页面。

**Architecture:** 保持后端接口不变，仅做前端路由与页面职责拆分。`/provider-models` 保留 provider 与模型挂载列表，新增独立页面承载 `创建 Provider` 和 `创建模型` 两段表单，并复用已有 `console-api` 能力。

**Tech Stack:** React, React Router, Vitest, 现有 `useRemoteData` 与 `console-api`

---

### Task 1: 拆分页面职责

**Files:**
- Modify: `web/src/pages/admin-provider-models.tsx`
- Create: `web/src/pages/admin-provider-model-create.tsx`
- Reuse: `web/src/lib/console-api.ts`

- [ ] 从 `admin-provider-models.tsx` 移除创建表单，仅保留统计、Provider 列表、模型挂载列表。
- [ ] 新建 `admin-provider-model-create.tsx`，迁移 `创建 Provider`、`创建模型`、状态提示与刷新逻辑。
- [ ] 复用现有 `getProviderModels`、`createProvider`、`createProviderModel`，不新增后端 API。

### Task 2: 调整 admin 路由和导航

**Files:**
- Modify: `web/src/app/router.tsx`

- [ ] 保留 `/provider-models` 作为“后台模型”只读页。
- [ ] 新增左侧导航项“新建模型”，例如路径 `/provider-model-create`。
- [ ] 路由标题与描述分别贴合“查看”与“创建”场景。

### Task 3: 补前端回归测试

**Files:**
- Modify: `web/src/test/router.test.tsx`

- [ ] 更新原“后台模型页支持先创建 provider 再创建 model”测试，使其访问新页面并断言标题为“新建模型”。
- [ ] 为只读“后台模型”页补最小断言，确认列表仍可展示，且不再依赖创建表单交互。
- [ ] 运行 `cd web && npm test -- src/test/router.test.tsx` 验证通过。
