# ranxi v2.24.0 至 v2.24.2 集成审查

比较基线：Even1997/main 的 d3096085b（PR #19）。上次是选择性移植 v2.23.1，旧版未移植项仍参见 [上次记录](ranxi-2.22-2.23-port.md)。

审查范围为三个发行标签之间的第一父链 PR。下表区分实际移植、设计上保留本地、未合入和发布元数据。不是上游完整版本，不能据此跳过以后对未移植项的比较。

## [v2.24.0](https://github.com/ranxi2001/sub2api/compare/v2.23.1...v2.24.0) · `bae943810`

| 上游 PR | 处理 | 选择依据 |
|---|---|---|
| [#957](https://github.com/ranxi2001/sub2api/pull/957) | 沿用 Even 已移植 | 质量确认和 Ciallo 检测分别见 1f3682660、1cc1590fc。 |
| [#958](https://github.com/ranxi2001/sub2api/pull/958) | 保留本地界面 | Even 的独立运营导航与上游渠道配置面板结构不同；保留原入口、筛选与跳转。 |
| [#960](https://github.com/ranxi2001/sub2api/pull/960) | 已适配 | 保留本地 New API 成本换算、余额币种与列表展示；基线 7ead57eb0，本轮补余额探测。 |
| [#961](https://github.com/ranxi2001/sub2api/pull/961) | 沿用 Even 已移植 | 保留请求回放的本地搜索规则，以及拒绝后不重新发送请求的压缩策略（fd9a0c58f、63b9bc173）。 |
| [#963](https://github.com/ranxi2001/sub2api/pull/963) | 沿用 Even 已移植 | 质量确认和 Ciallo 检测分别见 1f3682660、1cc1590fc。 |
| [#966](https://github.com/ranxi2001/sub2api/pull/966) | 保留本地续接策略 | 上游扩展 OAuth 工具结果的续接条件；保留本地的会话绑定和已交付历史证据。不声明两种协议等价。 |
| [#967](https://github.com/ranxi2001/sub2api/pull/967) | 已适配 | Qwen API Key 支持 DashScope、OpenAI 格式中继及固定时长协议；创建时保存每秒报价，查询与下载共用本地任务归属及一次结算。 |
| [#968](https://github.com/ranxi2001/sub2api/pull/968) | 已适配 | Qwen API Key 支持 DashScope、OpenAI 格式中继及固定时长协议；创建时保存每秒报价，查询与下载共用本地任务归属及一次结算。 |
| [#969](https://github.com/ranxi2001/sub2api/pull/969) | 不适用原补丁 | Even 的利润查询没有上游的 priced 查询结构及报表缓存共享请求机制，保留按当前配置估算的查询。 |
| [#970](https://github.com/ranxi2001/sub2api/pull/970) | 已适配 | Qwen API Key 支持 DashScope、OpenAI 格式中继及固定时长协议；创建时保存每秒报价，查询与下载共用本地任务归属及一次结算。 |
| [#965](https://github.com/ranxi2001/sub2api/pull/965) | 未合入 | 新的跨平台 gateway 账号涉及调度、请求平台归属、模型同步和采购成本快照（迁移 392）。本地按账号的采购成本和复合路由仍生效。 |
| [#964](https://github.com/ranxi2001/sub2api/pull/964) | 未合入 | 上游的 OAuth 429 独立观测需要四项迁移；387、389、390 已由本地不同功能占用。保留本地探测和恢复逻辑。 |
| [#972](https://github.com/ranxi2001/sub2api/pull/972) | 已适配 | 运维筛选/平台告警、测试标签、DeepSeek 内容拒绝分类及官方参考价、模型冷却处理。 |
| [#976](https://github.com/ranxi2001/sub2api/pull/976) | 已适配 | 运维筛选/平台告警、测试标签、DeepSeek 内容拒绝分类及官方参考价、模型冷却处理。 |
| [#974](https://github.com/ranxi2001/sub2api/pull/974) | 已适配 | Claude 账号可选请求最低扣费（默认关闭）；三个入口均按持久化回执返回用量，保留原始用量，账号设置并发更新按字段合并。 |
| [#973](https://github.com/ranxi2001/sub2api/pull/973) | 未合入 | 上游的 OAuth 429 独立观测需要四项迁移；387、389、390 已由本地不同功能占用。保留本地探测和恢复逻辑。 |
| [#975](https://github.com/ranxi2001/sub2api/pull/975) | 部分移植 | HTTP 和 WebSocket 重复字段校验、文本入口读体上限、Exa 搜索、Claude 测试采样、图片意图预筛和复制模型 ID。另移植 Antigravity 配额与转发使用同一端点、按有效时长计算 Token 速度、同账号 60 秒内重复手动刷新提醒、Grok 按上游搜索成功次数计费。该 PR 共 207 个文件，未整包合入。 |
| [#977](https://github.com/ranxi2001/sub2api/pull/977) | 已适配 | 运维筛选/平台告警、测试标签、DeepSeek 内容拒绝分类及官方参考价、模型冷却处理。 |
| [#978](https://github.com/ranxi2001/sub2api/pull/978) | 已适配 | Claude 账号可选请求最低扣费（默认关闭）；三个入口均按持久化回执返回用量，保留原始用量，账号设置并发更新按字段合并。 |
| [#979](https://github.com/ranxi2001/sub2api/pull/979) | 已适配 | 运维筛选/平台告警、测试标签、DeepSeek 内容拒绝分类及官方参考价、模型冷却处理。 |
| [#980](https://github.com/ranxi2001/sub2api/pull/980) | 已适配 | 模型广场支持展示币种、汇率与官方价格覆盖，保留 Even 的时段价和缓存价格。 |
| [#983](https://github.com/ranxi2001/sub2api/pull/983) | 已适配 | 运维筛选/平台告警、测试标签、DeepSeek 内容拒绝分类及官方参考价、模型冷却处理。 |
| [#982](https://github.com/ranxi2001/sub2api/pull/982) | 已适配 | 钱包结算准入，保留 Even 结算事务与软删除 Key 规则。 |
| [#986](https://github.com/ranxi2001/sub2api/pull/986) | 沿用 Even 已移植 | 保留请求回放的本地搜索规则，以及拒绝后不重新发送请求的压缩策略（fd9a0c58f、63b9bc173）。 |
| [#971](https://github.com/ranxi2001/sub2api/pull/971) | 未合入 | 新的跨平台 gateway 账号涉及调度、请求平台归属、模型同步和采购成本快照（迁移 392）。本地按账号的采购成本和复合路由仍生效。 |
| [#984](https://github.com/ranxi2001/sub2api/pull/984) | 部分移植 | 接入本地已实现入口的 Anthropic 媒体校验；未新增旧版本未接入的 Gemini Responses 路由。 |
| [#989](https://github.com/ranxi2001/sub2api/pull/989) | 已适配 | 运维筛选/平台告警、测试标签、DeepSeek 内容拒绝分类及官方参考价、模型冷却处理。 |
| [#987](https://github.com/ranxi2001/sub2api/pull/987) | 部分移植 | 修复请求捕获层传递流刷新错误。其余 Anthropic 原生流拼帧、未完整用量和缓存记账仍用本地实现；没有声明完整移植。 |
| [#990](https://github.com/ranxi2001/sub2api/pull/990) | 发布元数据 | 保留 Even 版本号和发布流程，不把选择性集成标记为对应上游发布。 |

## [v2.24.1](https://github.com/ranxi2001/sub2api/compare/v2.24.0...v2.24.1) · `0755d5ef0`

| 上游 PR | 处理 | 选择依据 |
|---|---|---|
| [#992](https://github.com/ranxi2001/sub2api/pull/992) | 不适用原补丁 | Even 的利润查询没有上游的 priced 查询结构及报表缓存共享请求机制，保留按当前配置估算的查询。 |
| [#991](https://github.com/ranxi2001/sub2api/pull/991) | 部分移植 | 修复请求捕获层传递流刷新错误。其余 Anthropic 原生流拼帧、未完整用量和缓存记账仍用本地实现；没有声明完整移植。 |
| [#993](https://github.com/ranxi2001/sub2api/pull/993) | 已适配 | 未使用配额重置时间、DeepSeek 流式事件归一化。 |
| [#994](https://github.com/ranxi2001/sub2api/pull/994) | 已适配 | 未使用配额重置时间、DeepSeek 流式事件归一化。 |
| [#996](https://github.com/ranxi2001/sub2api/pull/996) | 保留本地界面 | Even 的独立运营导航与上游渠道配置面板结构不同；保留原入口、筛选与跳转。 |
| [#995](https://github.com/ranxi2001/sub2api/pull/995) | 发布元数据 | 保留 Even 版本号和发布流程，不把选择性集成标记为对应上游发布。 |

## [v2.24.2](https://github.com/ranxi2001/sub2api/compare/v2.24.1...v2.24.2) · `991cd30e2`

| 上游 PR | 处理 | 选择依据 |
|---|---|---|
| [#998](https://github.com/ranxi2001/sub2api/pull/998) | 已移植 | 修正 Astra 生命周期测试时钟边界，不改变运行时逻辑。 |
| [#1002](https://github.com/ranxi2001/sub2api/pull/1002) | 已适配 | 只把已交付内容纳入桥接历史，保留本地 API Key 与会话绑定。 |
| [#1001](https://github.com/ranxi2001/sub2api/pull/1001) | 已适配 | 持久化结算的同用户批处理（每批至多 128）与恢复公平性；不增加另一个直接扣费入口。 |
| [#1000](https://github.com/ranxi2001/sub2api/pull/1000) | 保留本地调度 | 跨账号配额故障切换和同账号重试依赖另一套会话归属约束；保留现有的本地调度、隔离和预算规则。 |
| [#1004](https://github.com/ranxi2001/sub2api/pull/1004) | 部分移植 | 账号用量统计合并时长查询，保留 NULL 排除和原成本口径。不采用上游五分钟利润汇总（迁移 393）；历史明细清理与本地永久小时统计不同。 |
| [#1005](https://github.com/ranxi2001/sub2api/pull/1005) | 已适配 | 余额探测不依赖汇率字段。 |
| [#1008](https://github.com/ranxi2001/sub2api/pull/1008) | 已适配 | 持久化结算的同用户批处理（每批至多 128）与恢复公平性；不增加另一个直接扣费入口。 |
| [#1006](https://github.com/ranxi2001/sub2api/pull/1006) | 未合入 | 新的跨平台 gateway 账号涉及调度、请求平台归属、模型同步和采购成本快照（迁移 392）。本地按账号的采购成本和复合路由仍生效。 |
| [#1007](https://github.com/ranxi2001/sub2api/pull/1007) | 部分移植 | 对本地 HTTP 重新选号的路径，临时 OAuth 429 的同号重试只允许原发送账号；忙时等待原槽位或结束。保留已有明确配额耗尽切号、预算、会话隔离和已输出不重放规则。WebSocket 已直接重用原账号。 |
| [#1009](https://github.com/ranxi2001/sub2api/pull/1009) | 发布元数据 | 保留 Even 版本号和发布流程，不把选择性集成标记为对应上游发布。 |

## 验收与边界

- 在独立工作树保留账号批量自动重置卡：默认不修改，设置只接受 OpenAI OAuth 主账号，允许单独停用 5h 或 7d 窗口。没有对在线账号执行操作。
- [Wan 视频](qwen-wan-video.md)默认不启用；提交时锁定每秒单价，完成后用上游计费秒数结算，查询与下载共用一张账单。固定时长协议不接受参考视频。
- [Claude 账号最低扣费](features/account-probe-penalty.md)默认关闭。开启后也适用于普通的低金额 Token 请求；采购成本和原始 Token 不变。
- PostgreSQL 18 和 Redis 使用独立实例。结算回归覆盖并发领取、重复结算、事务回滚、软删除 Key、一次自动充值及恢复公平性；账号用量另测含 NULL 时长和空时间段。
- 前端组件测试覆盖账号批量编辑、用量显示、设置保存、展示币种和复制模型 ID；没有对接真实支付、外部模型或 Exa 账号。
- 没有增加数据库迁移、修改发布工作流或覆盖线上数据。为未移植的账务与调度功能，本记录不提供生产兼容性结论。
- 两个同优先级可用账号的回归覆盖原账号并发槽占满、恢复和客户端取消，分别使用 0 和 2 的切号预算；另测明确配额耗尽仍可正常切号。
- PostgreSQL 验证按总输出 / 总有效时长计算速度；Grok 使用模拟上游覆盖三种接口的流式与非流式回执，以及 Chat 直连路径。有效明细里的 0 优先；缺少有效明细保留原计数。
- 重复手动刷新只提示，不拦截。账号页该项和部分刷新回归通过；该文件另有 5 条既有并发升级测试引用未定义的 getManagementCapabilities，在此轮改动前的测试文件复跑同样失败。
