# ranxi v2.20.5 选择性集成记录

日期：2026-11-11。结论：本轮范围可交付（自评 9.4/10），未部署、未修改生产支付或 worker 配置。

## 来源与范围

- GitHub releases 最新复核：v2.20.5，发布于 2026-11-10 20:02:53 UTC（北京时间 11 月 11 日 04:02:53）。来源：https://github.com/ranxi2001/sub2api/releases/tag/v2.20.5 。
- 上游生产分支固定提交：924d4eecd；比较范围 a010f0846..upstream/releases/v2.20.5。upstream/main 是旧版本，不能作为本次来源。
- 原工作区 HEAD：32a1ee5d3；隔离区原改动快照：6b2cbec7a。选择性移植，保留本地版本号 2.19.0，不能据此宣称整仓与上游 v2.20.5 完全一致。
- 原工作区已有云猫微信/支付宝、回调、退款、内容审核、OAuth worker 等未提交改动。本轮先验证其原始 diff 和空暂存区未变化，再以相对快照的增量补丁合入；不提交这些用户改动。

## 导入项目

| PR | 内容 |
| --- | --- |
| #769 | 原账号归属下加密代理历史的原生压缩 |
| #766 | Codex 默认模型取自目录 |
| #767 | WS 首帧断开和超时日志分类 |
| #773 | 图片调度候选授权字段 |
| #772 | 基于可信自身 OAuth 401 的质量规则清理 |
| #770 | Companion Page 上下文规范化 |
| #774 | OAuth 401 与重登后的调度意图保留 |
| #771 | 降智后的原生可编辑控制与恢复 |
| #777 | WS SSE 无终态错误和区域归属边界 |
| #778 | 空快照、退休分组和调度协调 |
| #780 | 托管 BPS 连接的 Responses 准入 |
| #781 | 429 周额度阈值与 API Key 兜底 |
| #782 | 停调 API Key 候选、过滤和跨页选择 |
| #784 | 托管 BPS 缓存创建计费和授权状态显示 |

跳过 #775：Serverless 响应头超时补丁依赖本地已删除的实例注册及路由入口。强行移植会恢复大量不再使用的模块，本次保留现有实现；未修复此已移除功能。无关环境 CI 和上游版本号提交不导入。

## 冲突处理与附带修复

- 保留首次配置 account_settings、代理加载、shared/dedicated WS、本地 firstResponseEnabled 和 errorClose；不恢复顶层 0.07 成本倍率默认值。
- OAuth 401 自身凭据证据接入实际 openai_plugin_transport，不恢复已删除的旧 runtime fallback。插件接管、借票和动态绑定票据不认作自身撤销证据。
- 页面上下文统一使用严格规范化；保留 previous_response_id 非字符串拒绝边界。
- 429 门槛只接真实 HTTP 429 和明确周窗口；不把 BPS HTTP 200 内嵌 429、repair 改写视为 Codex 周额度证据。
- 保留本地 WS 无终态 server_error 错误规范，并验证不会切号、重放或重复结算。
- 数据库实测暴露本地分组限制跨连接自锁：CreateWithAccountGroups、BindGroups、SetGroupAllowedModels 改用同一事务读写；分组限制与调度 outbox 原子提交。新增 TestGroupAllowedModelsTransactionBoundaries 覆盖事务隔离、创建、重绑和回滚。
- 本轮新增迁移重编号为 344_oauth_degradation_native_controls.sql、345_quality_oauth_401_streaks.sql、346_quality_oauth_401_trusted_revocation.sql，避免本地 338/339 冲突；历史迁移不改写。
- 上游测试构造器适配本地签名，补齐旧 EditAccountModal 替身 modelMappings 声明；浏览器验收脚本适配本地代理接口、系统 Chrome 和 Windows Tailwind 扫描。

## 验证证据

| 验证 | 结果及边界 |
| --- | --- |
| 后端构建 | go build ./... 通过 |
| 前端构建 | pnpm build 通过，含 i18n、vue-tsc、Vite |
| 核心后端定向回归 | 656 项（含子测试），无失败/跳过 |
| 按 diff 提取的新增后端回归 | 391 项（含子测试），无失败/跳过；与上一行重叠，不能相加 |
| 真实 PostgreSQL / Redis 仓储回归 | 230 项（59 顶层测试），无失败/跳过；全部 36 个本轮新增顶层集成测试均执行通过 |
| 支付后端 | provider 包通过；service 定向 55 项（含子测试）通过 |
| 前端功能回归 | 10 个相关 spec 共 521 项通过；另 i18n 3 项、支付 8 个 spec 共 113 项通过 |
| Chrome 浏览器 | 实际 AutoConfigView 和子组件，虚构 API；验证停调候选、过滤、跨页勾选、100 上限、保存重载、放弃未保存编辑、清空与 390px 无横向溢出；已查看桌面及手机 PNG |
| 工作区保护 | 回灌前确认原 HEAD、原始 tracked diff、暂存区一致；增量补丁先 git apply --check；不改生产资源 |

数据库二进制在 Windows 交叉编译为 Linux amd64，使用 40 上独立、临时 PostgreSQL 18 / Redis 8 容器；未使用生产 DSN。Redis ACL 故障注入会出现预期 NOPERM 日志，最终测试判定 PASS。没有进行并发压测、外部付费调用、线上充值或端到端发布验证。

本机 Windows esbuild 临时文件权限问题通过既有临时预加载脚本启用标准 pipe fallback，未改依赖。浏览器使用系统 Chrome；默认 Playwright headless shell 未安装。

本机证据目录 C:/Users/Even/AppData/Local/Temp/：

- sub2api-v2205-core-results.jsonl、sub2api-v2205-added-results.jsonl、sub2api-v2205-payment-results.jsonl。
- sub2api-v2205-postgres-final.log（最终数据库测试日志，以 PASS 结束）。
- sub2api-v2205-browser-evidence/manifest.json 和 after-fallback-*.png。
- sub2api-v2205-tests-SKjgr1/overlay.json（下述兼容排除清单）。

## 未覆盖与发布限制

完整默认或 unit 标签套件未全绿，不能宣称全量测试通过。旧测试引用已删除接口，定向回归使用临时 overlay 排除以下既有编译债务（不提交 overlay，也不删除源码）：

- service：api_key_long_context_forwarding_test.go、gateway_forward_compat_error_failover_test.go、openai_gateway_anti_distillation_test.go、openai_gateway_mistral_test.go、openai_legacy_admission_rebind_test.go、openai_gateway_mistral_deepseek_effort_test.go、openai_gateway_mistral_request_pipeline_test.go。
- repository：gateway_cache_legacy_admission_test.go。
- handler：api_key_policy_forwarding_test.go、openai_issue715_ws_test.go、openai_issue758_recovery_test.go、pr612_header_timeout_test.go、pr714_image_header_timeout_test.go。
- 内部仓储集成二进制另隔离 20 个 package repository_test 外部 HTTP 集成文件：这些用例存在旧构造器接口不匹配；本轮 36 个新增内部仓储集成测试全部执行，未跳过。

此外，Windows 下既有 TestBPSHTTP2PrematureCloseFallsBackWithoutReplaying/before_headers 的 wsarecv 行为断言失败，TestSchedulerReadinessRealRedis 依赖 Unix socket 而失败；默认 service 全套也有失败，未完成逐项归因。这些是本轮完整验收的剩余缺口，需另行统一旧测试接口并在 Linux 复跑全套及 race 检查。上游新 CI 同样可能受既有 unit 编译债务影响。

迁移和规则处置存在持久效果：降智控制迁移后旧二进制仍可能重新强制历史限制；规则清理不会因回退二进制自动恢复。实际发布前应备份数据库、统一升级执行检测任务的节点，并验证迁移及回滚策略。本轮未执行发布。

## 质量自评

这是基于上述证据的范围内自评，不是外部质量认证；整体 9.4/10，可交付。

| 维度 | 分数 | 依据与改进点 |
| --- | --- | --- |
| 功能 | 9.4 | 定向、真实数据库及支付回归通过；完整套件和生产发布未验证 |
| 代码 | 9.4 | 保留定制、修复事务根因；旧接口测试债务仍待统一 |
| UI | 9.5 | 桌面/手机实图检查通过，未改整体设计 |
| UX | 9.5 | 分页、搜索、选择上限及持久化验证通过 |
| 美术 | N/A | 本次是管理界面功能移植，没有新增美术资产 |
| 性能 | 9.3 | 构建和定向运行正常；未做负载、延迟和 race 压测 |
| 安全 | 9.4 | 401 可信证据、CAS、事务回滚回归通过；全套与真实发布边界未覆盖 |
