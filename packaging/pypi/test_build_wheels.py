import io
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path

import build_wheels as bw


def fake_archives(folder, version):
    for (goos, goarch) in bw.PLATFORMS:
        name = f"jelly-dev_{version}_{goos}_{goarch}"
        content = f"binary-{goos}-{goarch}".encode()
        if goos == "windows":
            with zipfile.ZipFile(folder / f"{name}.zip", "w") as z:
                z.writestr("jd.exe", content)
                z.writestr("README.md", "x")
        else:
            with tarfile.open(folder / f"{name}.tar.gz", "w:gz") as t:
                info = tarfile.TarInfo("jd")
                info.size = len(content)
                t.addfile(info, io.BytesIO(content))


class BuildWheelsTest(unittest.TestCase):
    def test_version_must_be_a_release(self):
        self.assertEqual(bw.normalize_version("v1.2.3"), "1.2.3")
        for bad in ("main", "v1.2", "v1.2.3-rc1", ""):
            with self.assertRaises(ValueError):
                bw.normalize_version(bad)

    def test_one_wheel_per_platform_with_binary_and_valid_record(self):
        with tempfile.TemporaryDirectory() as tmp:
            archives, out = Path(tmp) / "archives", Path(tmp) / "out"
            archives.mkdir()
            fake_archives(archives, "1.2.3")
            self.assertEqual(bw.main(["--version", "v1.2.3", "--archives", str(archives), "--out", str(out)]), 0)
            wheels = sorted(out.glob("*.whl"))
            self.assertEqual(len(wheels), len(bw.PLATFORMS))
            names = {w.name for w in wheels}
            self.assertIn("jelly_dev-1.2.3-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.musllinux_1_1_x86_64.whl", names)
            self.assertIn("jelly_dev-1.2.3-py3-none-win_arm64.whl", names)
            for wheel in wheels:
                with zipfile.ZipFile(wheel) as z:
                    members = z.namelist()
                    script = "jd.exe" if "win" in wheel.name else "jd"
                    self.assertIn(f"jelly_dev-1.2.3.data/scripts/{script}", members)
                    mode = z.getinfo(f"jelly_dev-1.2.3.data/scripts/{script}").external_attr >> 16
                    self.assertTrue(mode & 0o111, "the binary must be executable")
                    record = z.read("jelly_dev-1.2.3.dist-info/RECORD").decode().splitlines()
                    listed = {line.split(",")[0]: line.split(",") for line in record}
                    for name in members:
                        self.assertIn(name, listed)
                        if name.endswith("RECORD"):
                            continue
                        data = z.read(name)
                        self.assertEqual(listed[name][1], bw.record_hash(data))
                        self.assertEqual(listed[name][2], str(len(data)))
                    meta = z.read("jelly_dev-1.2.3.dist-info/METADATA").decode()
                    self.assertIn("Name: jelly-dev\nVersion: 1.2.3\n", meta)
                    self.assertIn("Requires-Python: >=3.8", meta)

    def test_binary_content_comes_from_the_archive_of_the_platform(self):
        with tempfile.TemporaryDirectory() as tmp:
            archives, out = Path(tmp) / "archives", Path(tmp) / "out"
            archives.mkdir()
            fake_archives(archives, "1.2.3")
            bw.main(["--version", "v1.2.3", "--archives", str(archives), "--out", str(out)])
            wheel = next(out.glob("*macosx_11_0_arm64.whl"))
            with zipfile.ZipFile(wheel) as z:
                self.assertEqual(z.read("jelly_dev-1.2.3.data/scripts/jd"), b"binary-darwin-arm64")

    def test_archive_without_the_binary_names_the_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / "jelly-dev_1.2.3_linux_amd64.tar.gz"
            with tarfile.open(archive, "w:gz") as t:
                info = tarfile.TarInfo("README.md")
                t.addfile(info, io.BytesIO(b""))
            with self.assertRaises(SystemExit) as caught:
                bw.read_binary(archive, "linux")
            self.assertIn(archive.name, str(caught.exception))

    def test_license_is_part_of_the_wheel(self):
        with tempfile.TemporaryDirectory() as tmp:
            archives, out = Path(tmp) / "archives", Path(tmp) / "out"
            archives.mkdir()
            fake_archives(archives, "1.2.3")
            bw.main(["--version", "v1.2.3", "--archives", str(archives), "--out", str(out)])
            with zipfile.ZipFile(next(out.glob("*win_amd64.whl"))) as z:
                self.assertIn("jelly_dev-1.2.3.dist-info/LICENSE", z.namelist())
                self.assertNotIn("License-File", z.read("jelly_dev-1.2.3.dist-info/METADATA").decode())

    def test_missing_archive_or_wrong_version_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            archives = Path(tmp) / "archives"
            archives.mkdir()
            fake_archives(archives, "1.2.3")
            next(archives.glob("*windows_arm64*")).unlink()
            with self.assertRaises(SystemExit):
                bw.main(["--version", "v1.2.3", "--archives", str(archives), "--out", str(Path(tmp) / "out")])
            with self.assertRaises(SystemExit):
                bw.main(["--version", "v1.2.4", "--archives", str(archives), "--out", str(Path(tmp) / "out")])


if __name__ == "__main__":
    unittest.main()
