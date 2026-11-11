# Sub2API 桌面监控（Windows）

紧凑双列、置顶、拖动、最小化；两页分别显示实时概览与今日用户排行。
使用 Windows Edge WebView2，不依赖 Python 安装即可运行打包版本。

## 启动

运行 `Sub2Monitor.exe`，通过右上角设置按钮选择 `.env` 文件。
也可以将 `.env` 放在 exe 同目录，或用 `Sub2Monitor.exe --env C:\path\to\.env`。
配置示例见 `.env.example`，只需站点 HTTPS 根地址、管理密钥和分组 ID（默认 3）。
配置文件路径及置顶偏好保存在 `%APPDATA%/Sub2API Monitor/settings.json`；
密钥只在 Python 进程中使用，不传到界面、不写进程序包或日志。
程序仅调用 GET 接口，不改后台账号、代理、分组或设置。

## 数据口径

| 指标 | 后台接口与字段 |
| --- | --- |
| RPM / TPM、今日 Token、三个金额 | `dashboard/snapshot-v2` 的 `stats`：`rpm` / `tpm` / `today_tokens` / `today_actual_cost` / `today_account_cost` / `today_cost` |
| OpenAI 账号并发 | `ops/concurrency` 的 `platform.openai.current_in_use`，直接使用服务端聚合数 |
| 首字 | `ops/dashboard/overview?time_range=5m` 的 `ttft.avg_ms`，仅换算为秒 |
| 错误率 | 同接口 `error_rate`，仅格式化为百分比；异常数 `error_count_sla`，不将上游已恢复错误重新累计 |
| 分组可用账号 | `accounts/openai-summary?group=3` 的 `oauth_status.normal`（后台正常、可调度 OAuth 数，不含 API Key 账号） |
| 5h 预估 | 同接口 `oauth_quota["5h"]` 的 `remaining_cost / estimated_cost`，同时显示估算覆盖数；不重新计算额度 |
| 今日排行 | `dashboard/user-breakdown`，北京时间当天、全部分组，`sort_by=total_tokens&limit=50`；使用返回顺序、`email`、`user_id`、`total_tokens`、`actual_cost` |

概览每 5 秒读取一次；排行仅在打开时读取，之后每 30 秒读取。
失败保留上次值但明确标记过期；未返回的字段显示 `—`，不伪装为 0。
北京时间跨日后不再显示昨日的 Token、金额与排行；新数据就绪后恢复显示。
界面展示的是后台提供的统计，后台缓存仍可能有自身更新延迟。

## 从源码运行、测试和构建

```powershell
python -m venv .venv
.venv/Scripts/python -m pip install -r requirements.txt
.venv/Scripts/python -m unittest -v
.venv/Scripts/python monitor.py --env C:/path/to/.env
./build.ps1 -Python .venv/Scripts/python.exe
```

只读接入检查：`python monitor.py --env C:/path/to/.env --check`。
该命令输出经过字段白名单过滤的统计和用户排行，可能含邮箱，不要上传到公开日志。
发布整个 `dist/Sub2Monitor` 文件夹（包括 `_internal`），不要把真实 `.env` 一起发布。
