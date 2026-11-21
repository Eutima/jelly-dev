#!/usr/bin/env python3
"""Build the PyPI wheels of Jelly-Dev from the release archives of GoReleaser.

`pip install jelly-dev` then puts the `jd` binary on the PATH, like scripts/install.sh does. There is one wheel per
platform (the binary is static, so one Linux wheel serves glibc and musl systems such as Alpine). Only the standard
library is used; a wheel is a zip file with a RECORD of hashes.

  python3 packaging/pypi/build_wheels.py --version v1.2.3 --archives dist/archives --out dist/wheels
"""

import argparse
import base64
import hashlib
import re
import stat
import sys
import tarfile
import zipfile
from pathlib import Path

NAME = "jelly-dev"
MODULE = "jelly_dev"
SUMMARY = "Terminal IDE and diff/merge tool with a vim-style editor (the jd command)"
HOMEPAGE = "https://github.com/Eutima/jelly-dev"
REPO_ROOT = Path(__file__).resolve().parents[2]

# GoReleaser (os, arch) -> wheel platform tags. The Go binaries are static: the Linux wheel runs on glibc and musl.
PLATFORMS = {
    ("linux", "amd64"): ["manylinux_2_17_x86_64", "manylinux2014_x86_64", "musllinux_1_1_x86_64"],
    ("linux", "arm64"): ["manylinux_2_17_aarch64", "manylinux2014_aarch64", "musllinux_1_1_aarch64"],
    ("darwin", "amd64"): ["macosx_11_0_x86_64"],
    ("darwin", "arm64"): ["macosx_11_0_arm64"],
    ("windows", "amd64"): ["win_amd64"],
    ("windows", "arm64"): ["win_arm64"],
}

ARCHIVE = re.compile(r"^jelly-dev_(?P<version>[^_]+)_(?P<os>[a-z]+)_(?P<arch>[a-z0-9]+)\.(?:tar\.gz|zip)$")

INIT = '''"""Jelly-Dev: the jd command (a terminal IDE and diff/merge tool). The binary is installed with this package."""

import os
import sys
import sysconfig

__version__ = "{version}"


def binary_path():
    """Return the path of the jd binary installed by pip (None when it cannot be found)."""
    name = "jd.exe" if sys.platform == "win32" else "jd"
    for scheme in (None, "posix_user", "nt_user"):
        try:
            directory = sysconfig.get_path("scripts", scheme) if scheme else sysconfig.get_path("scripts")
        except KeyError:
            continue
        candidate = os.path.join(directory, name)
        if os.path.isfile(candidate):
            return candidate
    return None
'''

MAIN = '''"""python -m jelly_dev runs the installed jd binary (for when the scripts folder is not on the PATH)."""

import os
import subprocess
import sys

from . import binary_path


def main():
    path = binary_path()
    if path is None:
        sys.stderr.write("jelly-dev: the jd binary was not found; reinstall with: pip install --force-reinstall jelly-dev\\n")
        return 1
    if sys.platform == "win32":
        return subprocess.call([path, *sys.argv[1:]])
    os.execv(path, [path, *sys.argv[1:]])
    return 0


if __name__ == "__main__":
    sys.exit(main())
'''


def normalize_version(tag):
    """v1.2.3 -> 1.2.3; anything that is no release version is refused."""
    version = tag[1:] if tag.startswith("v") else tag
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise ValueError(f"not a release version: {tag!r} (expected vX.Y.Z)")
    return version


def record_hash(data):
    digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode("ascii")
    return f"sha256={digest}"


def read_binary(archive, goos):
    """Return the bytes of the jd binary inside a GoReleaser archive."""
    wanted = "jd.exe" if goos == "windows" else "jd"
    try:
        if archive.suffix == ".zip":
            with zipfile.ZipFile(archive) as z:
                return z.read(wanted)
        with tarfile.open(archive) as t:
            member = t.extractfile(wanted)
            if member is None:
                raise KeyError(wanted)
            return member.read()
    except KeyError:
        raise SystemExit(f"error: {wanted} is not in {archive}") from None


def metadata(version):
    readme = (REPO_ROOT / "README.md").read_text(encoding="utf-8")
    lines = [
        "Metadata-Version: 2.1",
        f"Name: {NAME}",
        f"Version: {version}",
        f"Summary: {SUMMARY}",
        f"Home-page: {HOMEPAGE}",
        "License: MIT",
        "Requires-Python: >=3.8",
        "Classifier: Environment :: Console",
        "Classifier: License :: OSI Approved :: MIT License",
        "Classifier: Operating System :: MacOS",
        "Classifier: Operating System :: Microsoft :: Windows",
        "Classifier: Operating System :: POSIX :: Linux",
        "Classifier: Programming Language :: Go",
        "Classifier: Topic :: Software Development",
        f"Project-URL: Source, {HOMEPAGE}",
        f"Project-URL: Issues, {HOMEPAGE}/issues",
        "Description-Content-Type: text/markdown",
        "",
        readme,
    ]
    return "\n".join(lines).encode("utf-8")


def build_wheel(binary, goos, goarch, version, out_dir):
    """Write the wheel for one platform and return its path."""
    tags = PLATFORMS[(goos, goarch)]
    dist_info = f"{MODULE}-{version}.dist-info"
    data = f"{MODULE}-{version}.data"
    script = "jd.exe" if goos == "windows" else "jd"
    files = [
        (f"{MODULE}/__init__.py", INIT.format(version=version).encode(), 0o644),
        (f"{MODULE}/__main__.py", MAIN.encode(), 0o644),
        (f"{data}/scripts/{script}", binary, 0o755),
        (f"{dist_info}/METADATA", metadata(version), 0o644),
        (f"{dist_info}/LICENSE", (REPO_ROOT / "LICENSE").read_bytes(), 0o644),
        (
            f"{dist_info}/WHEEL",
            ("Wheel-Version: 1.0\nGenerator: jelly-dev build_wheels\nRoot-Is-Purelib: false\n"
             + "".join(f"Tag: py3-none-{tag}\n" for tag in tags)).encode(),
            0o644,
        ),
    ]
    record = "".join(f"{name},{record_hash(content)},{len(content)}\n" for name, content, _ in files)
    record += f"{dist_info}/RECORD,,\n"
    files.append((f"{dist_info}/RECORD", record.encode(), 0o644))

    out_dir.mkdir(parents=True, exist_ok=True)
    path = out_dir / f"{MODULE}-{version}-py3-none-{'.'.join(tags)}.whl"
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        for name, content, mode in files:
            info = zipfile.ZipInfo(name, date_time=(2020, 1, 1, 0, 0, 0))  # fixed time: the same input gives the same wheel
            info.external_attr = (stat.S_IFREG | mode) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, content)
    return path


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--version", required=True, help="release tag, for example v1.2.3")
    parser.add_argument("--archives", type=Path, required=True, help="folder with the GoReleaser archives")
    parser.add_argument("--out", type=Path, required=True, help="folder for the wheels")
    args = parser.parse_args(argv)

    version = normalize_version(args.version)
    built = []
    for archive in sorted(args.archives.iterdir()):
        match = ARCHIVE.match(archive.name)
        if not match or (match["os"], match["arch"]) not in PLATFORMS:
            continue
        if match["version"] != version:
            raise SystemExit(f"error: {archive.name} is not version {version}")
        built.append(build_wheel(read_binary(archive, match["os"]), match["os"], match["arch"], version, args.out))
    if len(built) != len(PLATFORMS):
        raise SystemExit(f"error: built {len(built)} wheels, expected {len(PLATFORMS)}: archives are missing in {args.archives}")
    for wheel in built:
        print(wheel)
    return 0


if __name__ == "__main__":
    sys.exit(main())
