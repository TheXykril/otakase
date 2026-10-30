import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class BuildCoreTest(unittest.TestCase):
    def build(self, version, *args):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            script = root / "app/scripts/build-core.sh"
            script.parent.mkdir(parents=True)
            shutil.copyfile(Path(__file__).with_name("build-core.sh"), script)
            script.chmod(0o755)
            binary = root / "bin"
            binary.mkdir()
            tools = root / "gopath/bin"
            tools.mkdir(parents=True)
            calls = root / "calls"
            go = binary / "go"
            go.write_text('''#!/usr/bin/env bash
case "$*" in
  'env GOVERSION') printf '%s\\n' "$TEST_GO_VERSION" ;;
  'env GOPATH') printf '%s\\n' "$TEST_GO_PATH" ;;
  install*) printf '%s\\n' "$*" >> "$TEST_GO_CALLS" ;;
  *) exit 1 ;;
esac
''')
            go.chmod(0o755)
            mobile = tools / "gomobile"
            mobile.write_text('''#!/usr/bin/env bash
printf '%s\\n' "$*" >> "$TEST_GO_CALLS"
''')
            mobile.chmod(0o755)
            env = {
                **os.environ,
                "PATH": f"{binary}:{os.environ['PATH']}",
                "ANDROID_HOME": str(root / "sdk"),
                "JAVA_HOME": str(root / "jdk"),
                "TEST_GO_VERSION": version,
                "TEST_GO_PATH": str(root / "gopath"),
                "TEST_GO_CALLS": str(calls),
            }
            result = subprocess.run([str(script), *args], env=env, capture_output=True, text=True)
            return result, calls.read_text() if calls.exists() else ""

    def test_rejects_older_toolchains_before_building_the_core(self):
        for version in ("go1.25.9", "go1.26.0", "go1.26.1", "go1.26.2"):
            with self.subTest(version=version):
                result, calls = self.build(version)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Go 1.26.3 or newer", result.stderr)
                self.assertIn(version, result.stderr)
                self.assertEqual(calls, "")

    def test_builds_both_arm_targets_with_patched_runtimes(self):
        for version in ("go1.26.3", "go1.26.10", "go1.27.3"):
            with self.subTest(version=version):
                result, calls = self.build(version)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(calls.count("install "), 2)
                self.assertIn("bind -target=android/arm64,android/arm -androidapi 23", calls)

    def test_rejects_unrecognised_toolchains_before_installing_generators(self):
        result, calls = self.build("devel unknown")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Go 1.26.3 or newer", result.stderr)
        self.assertEqual(calls, "")

    def test_emulator_build_adds_x64_without_removing_arm_support(self):
        result, calls = self.build("go1.26.10", "--emulator")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("bind -target=android/arm64,android/arm,android/amd64 -androidapi 23", calls)

    def test_rejects_unknown_arguments_before_installing_generators(self):
        result, calls = self.build("go1.26.10", "--unknown")
        self.assertEqual(result.returncode, 2)
        self.assertIn("Usage:", result.stderr)
        self.assertEqual(calls, "")


if __name__ == "__main__":
    unittest.main()
