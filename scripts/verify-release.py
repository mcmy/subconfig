#!/usr/bin/env python3
"""Verify checksums and run the release binary matching the current host."""

import hashlib
import json
from pathlib import Path
import platform
import subprocess
import sys
import tarfile
import tempfile
import zipfile


def main():
    version = sys.argv[1]
    goos = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}[platform.system()]
    arch = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine().lower()]
    if len(sys.argv) > 2:
        assert f"{goos}/{arch}" == sys.argv[2], f"Unexpected runner platform: {goos}/{arch}"
    dist = Path("dist")
    for line in (dist / "SHA256SUMS").read_text().splitlines():
        digest, filename = line.split(maxsplit=1)
        assert hashlib.sha256((dist / filename).read_bytes()).hexdigest() == digest, filename

    name = f"subconfig_{version}_{goos}_{arch}"
    suffix = ".zip" if goos == "windows" else ".tar.gz"
    with tempfile.TemporaryDirectory(prefix="subconfig-verify-") as directory:
        if goos == "windows":
            with zipfile.ZipFile(dist / (name + suffix)) as archive:
                archive.extractall(directory)
        else:
            with tarfile.open(dist / (name + suffix)) as archive:
                # Keep compatibility with older Python versions on macOS.
                destination = Path(directory).resolve()
                for member in archive.getmembers():
                    path = (destination / member.name).resolve()
                    if destination not in path.parents or not (member.isfile() or member.isdir()):
                        raise ValueError(f"Unexpected archive member: {member.name}")
                options = {"filter": "data"} if hasattr(tarfile, "data_filter") else {}
                archive.extractall(directory, **options)
        root = Path(directory) / name
        binary = root / ("subconfig.exe" if goos == "windows" else "subconfig")

        def run(*args, **kwargs):
            return subprocess.run([str(binary), *args], cwd=root, check=True, stdout=subprocess.PIPE, **kwargs).stdout

        assert run("--version").decode().strip() == f"subconfig {version}"
        output = root / "converted.json"
        run("convert", "-c", "examples/clash.yaml", "-t", "sing-box", "-o", str(output))
        data = output.read_bytes()
        assert len(json.loads(data)["outbounds"]) > 0
        assert data == run("convert", "-c", "-", "-t", "sing-box", input=(root / "examples/clash.yaml").read_bytes())
        clash = run("convert", "-c", "examples/sing-box.json", "-t", "clash")
        assert b"proxies:" in clash
        print(f"Checksums, version, file conversion and stdin passed: {goos}/{arch}")


if __name__ == "__main__":
    main()
