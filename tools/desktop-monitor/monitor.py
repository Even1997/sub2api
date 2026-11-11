"""Read-only desktop monitor. Credentials never cross the webview bridge."""
from __future__ import annotations

import argparse
import copy
import json
import math
import os
from pathlib import Path
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
from urllib.parse import urlsplit

import requests

CHINA = timezone(timedelta(hours=8))
SECTIONS = ("stats", "ops", "capacity", "concurrency", "ranking")


def today():
    return datetime.now(CHINA).date().isoformat()


def number(value):
    return value if isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value) else None


def pick_numbers(data, keys):
    return {key: number(data.get(key)) for key in keys}


def project(section, data):
    """Explicit allowlist: no credentials, account details, or request contents."""
    if not isinstance(data, dict):
        raise ValueError("数据格式异常")
    if section == "stats":
        stats = data.get("stats")
        if not isinstance(stats, dict):
            raise ValueError("后台未返回统计")
        return pick_numbers(stats, ("rpm", "tpm", "today_tokens", "today_actual_cost", "today_account_cost", "today_cost")) | {
            "source_at": stats.get("stats_updated_at"), "source_stale": stats.get("stats_stale", False) is True}
    if section == "ops":
        return pick_numbers(data, ("error_rate", "error_count_sla", "request_count_sla")) | {
            "ttft_ms": number((data.get("ttft") or {}).get("avg_ms")), "source_at": data.get("end_time")}
    if section == "capacity":
        quota = (data.get("oauth_quota") or {}).get("5h") or {}
        return {"available": number((data.get("oauth_status") or {}).get("normal")),
                **pick_numbers(quota, ("remaining_cost", "estimated_cost", "estimated_accounts", "total_accounts"))}
    if section == "concurrency":
        platform = (data.get("platform") or {}).get("openai") or {}
        return {"used": number(platform.get("current_in_use")) if data.get("enabled") is True else None,
                "source_at": data.get("timestamp")}
    if section == "ranking":
        if not isinstance(data.get("users"), list):
            raise ValueError("后台未返回排行")
        return {"users": [{"user_id": number(row.get("user_id")), "email": str(row.get("email") or "未命名用户")[:254],
                           **pick_numbers(row, ("total_tokens", "actual_cost"))} for row in data["users"][:50]],
                "start_date": data.get("start_date"), "end_date": data.get("end_date")}
    raise ValueError("未知数据类型")


class Config:
    def __init__(self, path):
        self.path = Path(path).resolve()
        values = {}
        for line in self.path.read_text(encoding="utf-8-sig").splitlines():
            if "=" in line and not line.lstrip().startswith("#"):
                key, value = line.split("=", 1)
                values[key.strip()] = value.strip().strip('"').strip("'")
        self.base = values.get("SUB2API_BASE_URL", "").rstrip("/")
        self.key = values.get("SUB2API_ADMIN_API_KEY", "")
        url = urlsplit(self.base)
        if url.scheme != "https" or not url.hostname or url.username or url.password or url.query or url.fragment or url.path:
            raise ValueError("站点地址须为 HTTPS 根地址，例如 https://dogmaster.lol")
        if not self.key or "\n" in self.key or "\r" in self.key:
            raise ValueError("请填写 SUB2API_ADMIN_API_KEY")
        self.group = int(values.get("SUB2API_GROUP_ID", "3"))
        if self.group <= 0:
            raise ValueError("分组 ID 必须为正整数")
        self.host = url.hostname


class Client:
    def __init__(self, config):
        self.config = config

    def fetch(self, section, day):
        dates = {"start_date": day, "end_date": day, "timezone": "Asia/Shanghai"}
        routes = {
            "stats": ("dashboard/snapshot-v2", dates | {"include_stats": "true", "include_trend": "false", "include_model_stats": "false", "include_group_stats": "false", "include_users_trend": "false"}),
            "ops": ("ops/dashboard/overview", {"time_range": "5m"}),
            "capacity": ("accounts/openai-summary", {"group": str(self.config.group)}),
            "concurrency": ("ops/concurrency", {"platform": "openai"}),
            "ranking": ("dashboard/user-breakdown", dates | {"sort_by": "total_tokens", "limit": 50}),
        }
        path, params = routes[section]
        try:
            response = requests.get(self.config.base + "/api/v1/admin/" + path, params=params,
                                    headers={"x-api-key": self.config.key, "User-Agent": "Sub2API-Desktop-Monitor/1.0"},
                                    timeout=(5, 10), allow_redirects=False)
            if response.status_code != 200:
                labels = {401: "管理密钥无效", 403: "管理密钥缺少读取权限", 429: "后台限流，稍后重试"}
                raise ValueError(labels.get(response.status_code, f"后台 HTTP {response.status_code}"))
            envelope = response.json()
            if envelope.get("code") not in (0, 200, None):
                raise ValueError("后台接口返回异常")
            return project(section, envelope.get("data"))
        except requests.Timeout:
            raise ValueError("读取超时") from None
        except requests.RequestException:
            raise ValueError("无法连接后台") from None
        except (TypeError, KeyError, AttributeError):
            raise ValueError("数据格式异常") from None


class Monitor:
    def __init__(self, config=None):
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.wake = threading.Event()
        self.config = config
        self.generation = 0
        self.page = "overview"
        self.records = {}
        self.config_error = None

    def configure(self, config):
        with self.lock:
            self.config = config
            self.config_error = None
            self.generation += 1
            self.records = {}
        self.wake.set()

    def poll(self):
        with self.lock:
            config, generation, day = self.config, self.generation, today()
            if not config:
                return
            targets = [s for s in SECTIONS if s != "ranking"]
            ranking = self.records.get("ranking", {})
            if self.page == "ranking" and (ranking.get("day") != day or time.time() - ranking.get("attempt_at", 0) >= 30):
                targets.append("ranking")
        client = Client(config)
        def fetch(section):
            try:
                return section, client.fetch(section, day), None
            except Exception as exc:
                # Do not return raw HTTP bodies or exception strings containing credentials.
                safe = str(exc) if isinstance(exc, ValueError) and not isinstance(exc, requests.exceptions.JSONDecodeError) else "读取失败"
                return section, None, safe if len(safe) < 120 else "数据格式异常"
        with ThreadPoolExecutor(max_workers=5) as pool:
            for section, data, error in pool.map(fetch, targets):
                with self.lock:
                    if generation != self.generation:
                        continue
                    old = self.records.get(section, {})
                    self.records[section] = {"data": data if error is None else old.get("data"),
                        "updated_at": time.time() if error is None else old.get("updated_at"),
                        "day": day if error is None else old.get("day"), "attempt_at": time.time(), "error": error}

    def run(self):
        while not self.stop.is_set():
            self.wake.clear()
            started = time.monotonic()
            self.poll()
            self.wake.wait(max(0.5, 5 - (time.monotonic() - started)))

    def snapshot(self):
        with self.lock:
            records = copy.deepcopy(self.records)
            host = self.config.host if self.config else "未连接"
            group = self.config.group if self.config else 3
            configured, error = self.config is not None, self.config_error
        for section, record in records.items():
            age = time.time() - (record.get("updated_at") or 0)
            data = record.get("data") or {}
            record["stale"] = bool(record.get("error")) or age > (65 if section == "ranking" else 25) or data.get("source_stale", False)
            if section in ("stats", "ranking") and record.get("day") != today():
                record["data"] = None
                record["stale"] = True
            source_at = data.get("source_at")
            if source_at:
                try:
                    if time.time() - datetime.fromisoformat(source_at.replace("Z", "+00:00")).timestamp() > 120:
                        record["stale"] = True
                except (ValueError, TypeError):
                    record["stale"] = True
        return {"configured": configured, "config_error": error, "host": host, "group_id": group,
                "day": today(), "sections": records}


def settings_path():
    return Path(os.environ.get("APPDATA", str(Path.home()))) / "Sub2API Monitor" / "settings.json"


def read_settings():
    try:
        return json.loads(settings_path().read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}


def write_settings(updates):
    data = read_settings() | updates
    path = settings_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(".tmp")
    temp.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")
    temp.replace(path)


class Bridge:
    def __init__(self, monitor):
        self._monitor = monitor
        self._window = None
        self._pinned = read_settings().get("pinned", True)

    def snapshot(self):
        result = self._monitor.snapshot()
        result["pinned"] = self._pinned
        return result

    def page(self, page):
        if page in ("overview", "ranking"):
            with self._monitor.lock:
                self._monitor.page = page
            self._monitor.wake.set()

    def pin(self, enabled):
        # WinForms TopMost must be set on its UI thread. The library setter
        # touches it from the bridge thread and can deadlock WebView2.
        from System import Action
        self._pinned = bool(enabled)
        self._window.native.BeginInvoke(Action(lambda: setattr(self._window, "on_top", self._pinned)))
        write_settings({"pinned": self._pinned})
        return self._pinned

    def minimize(self):
        self._window.minimize()

    def close(self):
        self._window.destroy()

    def choose_config(self):
        import webview
        selected = self._window.create_file_dialog(webview.FileDialog.OPEN, allow_multiple=False, file_types=("配置文件 (*.env)", "所有文件 (*.*)"))
        if not selected:
            return False
        try:
            config = Config(selected[0])
            self._monitor.configure(config)
            write_settings({"env_path": str(config.path)})
            return True
        except (OSError, ValueError):
            with self._monitor.lock:
                self._monitor.config_error = "配置未加载，请检查 HTTPS 地址、管理密钥和分组 ID。"
            return False


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--env", type=Path)
    parser.add_argument("--check", action="store_true", help="Read once and print only projected metrics")
    args = parser.parse_args()
    settings = read_settings()
    default_env = Path(sys.executable if getattr(sys, "frozen", False) else __file__).parent / ".env"
    path = args.env or Path(settings.get("env_path") or default_env)
    monitor = Monitor()
    try:
        monitor.configure(Config(path))
    except (OSError, ValueError):
        monitor.config_error = "请选择包含站点地址和管理密钥的 .env 文件。"
    if args.check:
        monitor.page = "ranking"
        monitor.poll()
        print(json.dumps(monitor.snapshot(), ensure_ascii=True))
        return
    import webview
    bridge = Bridge(monitor)
    html = (Path(__file__).parent / "ui" / "index.html").read_text(encoding="utf-8")
    window = webview.create_window("狗王 · 桌面监控", html=html, js_api=bridge,
        width=420, height=650, min_size=(360, 540), frameless=True, easy_drag=False,
        on_top=settings.get("pinned", True), background_color="#ffffff", text_select=True)
    bridge._window = window
    window.events.closed += lambda: (monitor.stop.set(), monitor.wake.set())
    threading.Thread(target=monitor.run, name="monitor-poll", daemon=True).start()
    webview.start(gui="edgechromium", debug=False, private_mode=True)


if __name__ == "__main__":
    main()
