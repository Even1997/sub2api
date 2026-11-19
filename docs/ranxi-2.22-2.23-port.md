# 上游 v2.22.0、v2.23.0 选择性移植记录

基线：Even `780624b76`；最近一次已记录的上游集成为 v2.21.6。本记录不是全量发布声明，不能据此跳过这两个版本的剩余差异。应用版本号、数据库迁移及部署配置未改。

| 上游范围 | 本地提交 | 本次范围 |
| --- | --- | --- |
| [v2.21.6 → v2.22.0](https://github.com/ranxi2001/sub2api/compare/v2.21.6...v2.22.0) | `8ed718e04` | Gemini Carbon 目录、转换与默认价格；OpenAI/Anthropic 工具 schema；服务端搜索消息转换；Claude/Kimi 配额；Bedrock 渠道开关；支付宝退款幂等；HTML 缓存及上游连接保活。 |
| [v2.22.0 → v2.23.0](https://github.com/ranxi2001/sub2api/compare/v2.22.0...v2.23.0) | `4837a6601` | 协议转换包内的缓存读写总量与 TTL 明细、GPT-6.2 元数据和推理档位、VIP 垫付金额展示与编辑精度。 |

尚待整合的主要功能：

- v2.22.0：分组 Ultra Fast、Vertex 多区域、账单批处理、分组日汇总、账号平台切换及网关续接。整合结算前须覆盖本地「请求进行中删除 Key」和自定义价格场景；归档前须验证并发写入。
- v2.23.0：Ultra Fast 促销和长上下文结算、按请求 TTL 收费、利润钱包快照、迁移、逐账号同步白名单、Vertex Responses 转发及 WS 故障恢复。转换层能传递用量，不代表最终扣款已采用新规则。
- 上游还移除了凭证守护及打票管理入口，本批未移植该组删除。以上为主要差异，不是剩余提交的穷举。

回归入口（Go 命令从 backend 目录执行，pnpm 从 frontend 执行）：

~~~sh
go test -tags unit ./internal/pkg/apicompat ./internal/pkg/openai ./internal/pkg/antigravity ./internal/pkg/gemini ./internal/pkg/geminicli ./internal/domain ./internal/payment/provider
go test -tags unit -run 'Fable|Kimi|Rollover|Auth.*Identity|UpstreamDial|SanitizeOpenAIResponses|Carbon|GeminiCompatThinking' ./internal/service ./internal/repository
pnpm exec vitest run src/components/admin/user/__tests__/UserAutoCreditModal.spec.ts src/views/admin/__tests__/ChannelsView.bedrockCompat.spec.ts src/components/account/__tests__/CNProviderQuotaCell.spec.ts src/composables/__tests__/useModelWhitelist.spec.ts
~~~

带 embed 标签的 web 测试需要前端资源。主线的 `TestEmbeddedFrontendBypassesBareAPIAliases` 对 `/v3/other` 的期望与已有实现冲突；用原主线源文件独立复现。本批未修改该路由或断言。没有执行生产实扣、数据库升级或部署。
