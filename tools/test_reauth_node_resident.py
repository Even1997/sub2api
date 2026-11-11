import json
import shutil
import subprocess
import unittest
from pathlib import Path


@unittest.skipUnless(shutil.which("node"), "Node.js is required")
class ResidentReplyTests(unittest.TestCase):
    def test_missing_executable_and_success_each_reply_once(self):
        node = shutil.which("node")
        jobs = [
            {"command": ["sub2api-nonexistent-executable-for-test"], "timeout": 2},
            {"command": [node, "-e", "process.stdout.write('resident-ok')"], "timeout": 2},
        ]
        result = subprocess.run(
            [node, str(Path(__file__).with_name("reauth_node_resident.mjs"))],
            input="".join(json.dumps(job) + "\n" for job in jobs),
            capture_output=True, text=True, timeout=10, check=True,
        )
        replies = [json.loads(line) for line in result.stdout.splitlines()]
        self.assertEqual(len(replies), 2)
        self.assertEqual(sum(reply["returncode"] != 0 for reply in replies), 1)
        success = next(reply for reply in replies if reply["returncode"] == 0)
        self.assertEqual(success["stdout"], "resident-ok")


if __name__ == "__main__":
    unittest.main()
