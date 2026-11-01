import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("update_manifest", Path(__file__).with_name("update-manifest.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ManifestTest(unittest.TestCase):
    def test_metadata_describes_the_exact_apk_and_android_version(self):
        with tempfile.TemporaryDirectory() as folder:
            apk = Path(folder) / "app-release.apk"
            apk.write_bytes(b"signed APK fixture")
            data = module.manifest(apk, "0.1.0", 1245)
            self.assertEqual(data, {
                "schema": 1,
                "package_name": "com.xyverion.otakase",
                "version_name": "0.1.0",
                "version_code": 1245,
                "apk": "otakase-android.apk",
                "size": 18,
                "sha256": hashlib.sha256(apk.read_bytes()).hexdigest(),
            })
            for code in [0, -1, 2_100_000_001]:
                with self.assertRaises(ValueError):
                    module.manifest(apk, "0.1.0", code)
            apk.write_bytes(b"")
            with self.assertRaises(ValueError):
                module.manifest(apk, "0.1.0", 1245)

    def test_desktop_metadata_names_the_release_file(self):
        with tempfile.TemporaryDirectory() as folder:
            setup = Path(folder) / "setup.exe"
            setup.write_bytes(b"MZ installer")
            data = module.manifest(setup, "26.8.0", 26008000, "otakase-app-windows-setup.exe", "file")
            self.assertEqual(data["file"], "otakase-app-windows-setup.exe")
            self.assertNotIn("apk", data)
            self.assertEqual(data["size"], 12)
            self.assertEqual(data["sha256"], hashlib.sha256(b"MZ installer").hexdigest())


if __name__ == "__main__":
    unittest.main()
