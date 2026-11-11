import os
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from tools import openai_oauth_reauth_worker as worker


def parallel_probe(config, _stop=None):
    root = Path(config.base_url)
    (root / config.worker_id).touch()
    deadline = time.monotonic() + 3
    while len(list(root.glob("probe-slot-*"))) < 3 and time.monotonic() < deadline:
        time.sleep(0.01)
    if len(list(root.glob("probe-slot-*"))) == 3:
        (root / ("done-" + config.worker_id)).touch()
    time.sleep(0.4)


def failing_pool_probe(config, _stop=None):
    root = Path(config.base_url)
    pid_file = root / "descendant.pid"
    if config.worker_id.endswith("-slot-1"):
        deadline = time.monotonic() + 3
        while not pid_file.exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        os._exit(7)
    process = subprocess.Popen(["/bin/sleep", "30"])
    pid_file.write_text(str(process.pid), encoding="utf-8")
    process.wait()


def linux_process_running(pid):
    try:
        state = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8").split(") ", 1)[1][0]
    except FileNotFoundError:
        return False
    return state != "Z"


def holding_worker_probe(config, _stop=None):
    process = subprocess.Popen(["/bin/sleep", "30"])
    (Path(config.base_url) / (config.worker_id + ".pid")).write_text(str(process.pid), encoding="utf-8")
    process.wait()


def signal_pool_probe(directory):
    config = worker.WorkerConfig(directory, "x" * 32, "probe", None, concurrency=2)
    signal.signal(signal.SIGTERM, worker.terminate_worker_pool)
    with patch.object(worker.WorkerAPI, "runtime_concurrency", return_value=2), \
            patch.object(worker, "_run_worker_loop", holding_worker_probe):
        worker.run_worker_pool(config)


class ReauthWorkerPoolTests(unittest.TestCase):
    def test_config_concurrency_defaults_and_bounds(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "src").mkdir()
            for name in ("protocol-login.mjs", "tls-transport.mjs"):
                (root / "src" / name).touch()
            base = {"OPENAI_REAUTH_WORKER_TOKEN": "x" * 32, "TOSUB2_ROOT": directory}
            with patch.dict(os.environ, base, clear=True):
                self.assertEqual(worker.WorkerConfig.from_env().concurrency, 2)
            for value in ("0", "17", "-1", "2.5", "NaN"):
                with patch.dict(os.environ, dict(base, OPENAI_REAUTH_CONCURRENCY=value), clear=True):
                    with self.assertRaises(worker.WorkerError):
                        worker.WorkerConfig.from_env()
            for value in ("1", "3", "16"):
                with patch.dict(os.environ, dict(base, OPENAI_REAUTH_CONCURRENCY=value), clear=True):
                    self.assertEqual(worker.WorkerConfig.from_env().concurrency, int(value))

    def test_slot_ids_are_distinct_and_bounded(self):
        config = worker.WorkerConfig("http://127.0.0.1", "x" * 32, "x" * 128, None)
        ids = [worker.worker_slot_config(config, slot).worker_id for slot in range(1, 17)]
        self.assertEqual(len(set(ids)), 16)
        self.assertTrue(all(len(worker_id) <= 128 for worker_id in ids))

    def test_worker_slot_isolates_its_process_group_before_running(self):
        config = worker.WorkerConfig("http://127.0.0.1", "x" * 32, "test", None)
        with patch.object(worker.signal, "signal"), \
                patch.object(worker.os, "setpgid", create=True) as setpgid, \
                patch.object(worker, "_run_worker_loop") as run:
            worker._worker_loop(config)
        setpgid.assert_called_once_with(0, 0)
        run.assert_called_once_with(config, None)

    def test_pool_cleanup_signals_the_worker_process_group(self):
        child = Mock(pid=123)
        with patch.object(worker.os, "killpg", create=True) as killpg:
            worker.signal_worker_group(child, signal.SIGTERM)
        killpg.assert_called_once_with(123, signal.SIGTERM)
        child.terminate.assert_not_called()

    def test_pool_cleanup_falls_back_when_process_group_is_not_ready(self):
        child = Mock(pid=123)
        child.is_alive.return_value = True
        with patch.object(worker.os, "killpg", side_effect=ProcessLookupError, create=True):
            worker.signal_worker_group(child, signal.SIGTERM)
        child.terminate.assert_called_once_with()

    @unittest.skipUnless(sys.platform.startswith("linux"), "fork integration")
    def test_three_real_worker_processes_overlap(self):
        with tempfile.TemporaryDirectory() as directory:
            config = worker.WorkerConfig(directory, "x" * 32, "probe", None, concurrency=3)
            with patch.object(worker.WorkerAPI, "runtime_concurrency", return_value=config.concurrency), \
                    patch.object(worker, "_run_worker_loop", parallel_probe):
                with self.assertRaisesRegex(worker.WorkerError, "process exited"):
                    worker.run_worker_pool(config)
            self.assertEqual(len(list(Path(directory).glob("done-*"))), 3)

    @unittest.skipUnless(sys.platform.startswith("linux"), "process-group integration")
    def test_pool_failure_terminates_protocol_descendants(self):
        with tempfile.TemporaryDirectory() as directory:
            config = worker.WorkerConfig(directory, "x" * 32, "probe", None, concurrency=2)
            with patch.object(worker.WorkerAPI, "runtime_concurrency", return_value=config.concurrency), \
                    patch.object(worker, "_run_worker_loop", failing_pool_probe):
                with self.assertRaisesRegex(worker.WorkerError, "process exited"):
                    worker.run_worker_pool(config)
            pid = int((Path(directory) / "descendant.pid").read_text(encoding="utf-8"))
            deadline = time.monotonic() + 3
            while linux_process_running(pid) and time.monotonic() < deadline:
                time.sleep(0.02)
            running = linux_process_running(pid)
            if running:
                os.kill(pid, signal.SIGKILL)
            self.assertFalse(running)

    @unittest.skipUnless(sys.platform.startswith("linux"), "process-group integration")
    def test_parent_sigterm_stops_all_slots_and_protocol_descendants(self):
        with tempfile.TemporaryDirectory() as directory:
            process = worker.multiprocessing.get_context("fork").Process(target=signal_pool_probe, args=(directory,))
            process.start()
            try:
                deadline = time.monotonic() + 5
                while len(list(Path(directory).glob("*.pid"))) < 2 and time.monotonic() < deadline:
                    time.sleep(0.02)
                self.assertEqual(len(list(Path(directory).glob("*.pid"))), 2)
                process.terminate()
                process.join(timeout=8)
                self.assertFalse(process.is_alive())
                for path in Path(directory).glob("*.pid"):
                    self.assertFalse(linux_process_running(int(path.read_text(encoding="utf-8"))))
            finally:
                if process.is_alive():
                    process.kill()
                    process.join()
                for path in Path(directory).glob("*.pid"):
                    pid = int(path.read_text(encoding="utf-8"))
                    if linux_process_running(pid):
                        os.kill(pid, signal.SIGKILL)

    def test_once_does_not_start_a_pool(self):
        config = worker.WorkerConfig("http://127.0.0.1", "x" * 32, "test", None, concurrency=3)
        with patch("sys.argv", ["worker", "--once"]), \
                patch.object(worker.WorkerConfig, "from_env", return_value=config), \
                patch.object(worker.shutil, "which", return_value="node"), \
                patch.object(worker, "run_once", return_value=False) as run, \
                patch.object(worker, "run_worker_pool") as pool:
            self.assertEqual(worker.main(), 0)
            run.assert_called_once()
            pool.assert_not_called()


if __name__ == "__main__":
    unittest.main()
