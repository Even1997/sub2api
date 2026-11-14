# 按模型调整缓存读取计费

入口：管理后台 → 分组 → 新建/编辑 → 缓存读取降低比例下方的「按模型调整缓存读取」。

这是计费分类调整，不会改变上游实际缓存行为。下游 usage 显示调整后的计费口径；输出 token 不变。

## 规则与兼容

- 按客户端请求模型名精确匹配（忽略大小写、首尾空白），在渠道映射前选择规则。
- 命中模型规则优先；未配置模型继续用原分组默认比例。显式 0 表示该模型不调整。
- 固定模式直接使用 `percent`；动态模式额外设置 `start_hit_rate_percent`。
- 默认不新增任何模型规则。升级迁移不会改变现有分组计费。
- API 更新省略 `cache_read_reduction_rules` 保留原规则，传 `[]` 清空；最多 200 条，不接受重复模型或通配符。
- WebSocket 每轮使用本轮计费的分组快照；下一轮才应用管理员更新和模型切换。

动态公式（百分比值按 0–100 表示）：

```
命中率 = 原始缓存读取 / 原始总输入 × 100
实际降低比例 = 最大降低比例 × max(0, (命中率 - 起始命中率) / (100 - 起始命中率))
转入普通输入 = round(原始缓存读取 × 实际降低比例 / 100)
```

例如起始 80%、最大 5%：命中率 80%/90%/96%/100% 时实际降低比例为 0%/2.5%/4%/5%。
总输入 1,000、缓存 960 时，38 个缓存 token 转为普通输入，得到普通输入 78、缓存 922。这里的 4% 是缓存量的比例，不是总输入的 4 个百分点。

OpenAI 的 `input_tokens` / `prompt_tokens` 包含缓存，因此总输入保持不变，只修改缓存明细。Anthropic 的 `input_tokens` 不含缓存，因此普通输入增加相同数量。Anthropic 流式 usage 使用累计快照处理部分字段更新。

费用仍按各模型的普通输入、缓存、输出单价分别计算，再应用原分组倍率及既有金额精度；本功能不修改单价或分组倍率。

## 验证与边界

回归用例覆盖规则继承/覆盖/清空、模型映射、动态公式、输入校验、鉴权快照、下游 JSON/SSE、Anthropic 分段更新，以及真实本地 WebSocket 的 ctx_pool / passthrough 两种模式、多轮模型切换和规则更新。计费断言核对输入/缓存/输出、总费用和余额扣费命令。

迁移 `314_group_cache_read_reduction_rules.sql` 增加 JSONB 列，已用隔离 PostgreSQL 18 执行完整迁移和 repository 新增/读取/清空验证。常规仓储集成测试仍使用项目的 Docker 测试环境。

```powershell
# backend
go test -p 2 -tags=unit ./internal/service ./internal/handler ./internal/handler/admin ./internal/handler/dto -run 'Test(CacheReadReduction|ReduceCacheRead|ReduceClientVisibleSSE|OpenAIWSCacheRead)' -count=1
# frontend
pnpm exec vitest run src/components/admin/group/__tests__/CacheReadReductionRulesEditor.spec.ts src/views/admin/__tests__/GroupsView.cacheReadReduction.spec.ts
pnpm run build
# 仓库根目录；只预览编辑组件，不连接线上或保存分组
node .github/scripts/cache-read-rules-browser.mjs --serve
```

浏览器检查可去掉 `--serve`；需通过 `PLAYWRIGHT_ROOT` 指向已有 Playwright 安装目录。覆盖中文/英文和 390/1440 像素宽度。

本次结果：上述后端定向回归、WebSocket handler 回归、前端 8 项测试、TypeScript 检查、生产构建和组件浏览器检查通过。Windows 本机的 esbuild 临时文件删除失败，生产构建使用其 inline transform 传输回退完成，编译和压缩配置不变。没有修改共享依赖。竞态检查因本机内存不足中止，未记为通过。

尚未做线上流量回放或压力测试。本次验证重点为 OpenAI/Anthropic 接口；Gemini 原生 usageMetadata 及特殊计费覆盖（如强制缓存计费、身份注入用量扣减）不能据此宣称与下游完全一致。

原始上游字节在解析和下游改写时保持独立，但没有新增永久保存原始 usage 的数据库列；常规用量日志仍保存计费口径。

发布需先应用迁移。旧版本忽略模型规则并恢复使用分组默认比例，回滚会改变这些模型的计费结果，回滚前应核对分组默认值。
