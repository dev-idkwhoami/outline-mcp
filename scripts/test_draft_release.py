"""Exercise the release script using a fake gh; no GitHub credentials or writes."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("draft-release.sh").resolve()
ASSETS = [
    "outline-mcp-linux-amd64", "outline-mcp-windows-amd64.exe",
    "LICENSE", "THIRD_PARTY_NOTICES.txt", "build-info.json",
]
FAKE_GH = r'''
import json, os, pathlib, shutil, sys
args = sys.argv[1:]
with open(os.environ["GH_LOG"], "a") as log:
    log.write(json.dumps(args) + "\n")
scenario = os.environ["SCENARIO"]
if args[0] == "api":
    if scenario == "api-error":
        sys.exit(1)
    if scenario in ("draft", "published"):
        print(json.dumps({"tag_name": os.environ["RELEASE_TAG"], "draft": scenario == "draft"}))
elif args[:2] == ["release", "view"]:
    print("true")
elif args[:2] == ["release", "download"]:
    name = args[args.index("--pattern") + 1]
    dest = pathlib.Path(args[args.index("--dir") + 1]) / name
    shutil.copyfile(pathlib.Path("bin") / name, dest)
    if scenario == "corrupt-download" and name == "outline-mcp-linux-amd64":
        with dest.open("ab") as f:
            f.write(b"tampered")
elif args[:2] not in (["release", "create"], ["release", "upload"]):
    raise RuntimeError(args)
'''


@unittest.skipUnless(shutil.which("bash") and shutil.which("jq") and shutil.which("sha256sum"),
                     "requires bash, jq, and sha256sum")
class DraftReleaseTests(unittest.TestCase):
    def run_scenario(self, scenario, tag="v0.1.0"):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "bin").mkdir()
            (root / "tools").mkdir()
            (root / "runner-temp").mkdir()
            manifest = []
            for name in ASSETS:
                data = ("test asset " + name).encode()
                (root / "bin" / name).write_bytes(data)
                manifest.append(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
            (root / "bin" / "SHA256SUMS").write_text("".join(manifest))
            fake = root / "tools" / "gh"
            fake.write_text(f"#!{sys.executable}\n" + FAKE_GH)
            fake.chmod(0o755)
            log = root / "gh-log.jsonl"
            env = {**os.environ, "PATH": str(root / "tools") + os.pathsep + os.environ["PATH"],
                   "GH_REPO": "example/outline-mcp", "RELEASE_TAG": tag,
                   "RUNNER_TEMP": str(root / "runner-temp"), "GH_LOG": str(log),
                   "SCENARIO": scenario}
            result = subprocess.run(["bash", str(SCRIPT)], cwd=root, env=env,
                                    text=True, capture_output=True, timeout=20)
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            return result, calls

    def test_new_release_is_draft_with_verified_tag(self):
        result, calls = self.run_scenario("new")
        self.assertEqual(result.returncode, 0, result.stderr)
        create = next(c for c in calls if c[:2] == ["release", "create"])
        self.assertIn("--draft", create)
        self.assertIn("--verify-tag", create)
        self.assertNotIn("--prerelease", create)
        self.assertEqual(sum(c[:2] == ["release", "download"] for c in calls), 6)

    def test_unprefixed_version_tag(self):
        result, calls = self.run_scenario("new", "1.0.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        create = next(c for c in calls if c[:2] == ["release", "create"])
        self.assertEqual(create[2], "1.0.0")
        self.assertIn("--draft", create)

    def test_prerelease_tag(self):
        result, calls = self.run_scenario("new", "v0.2.0-rc.1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--prerelease", next(c for c in calls if c[:2] == ["release", "create"]))

    def test_existing_draft_is_updated(self):
        result, calls = self.run_scenario("draft")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(any(c[:2] == ["release", "upload"] for c in calls))
        self.assertFalse(any(c[:2] == ["release", "create"] for c in calls))

    def test_published_release_and_failed_read_are_not_mutated(self):
        for scenario in ("published", "api-error"):
            with self.subTest(scenario=scenario):
                result, calls = self.run_scenario(scenario)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any(c[:2] in (["release", "create"], ["release", "upload"])
                                     for c in calls))

    def test_corrupt_download_fails_verification(self):
        result, _ = self.run_scenario("corrupt-download")
        self.assertNotEqual(result.returncode, 0)

    def test_invalid_tag_aborts_before_api(self):
        result, calls = self.run_scenario("new", "not-a-version")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])


if __name__ == "__main__":
    unittest.main()
