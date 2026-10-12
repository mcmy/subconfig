#!/usr/bin/env python3
"""Build release archives from the repository root using Go and Python 3."""

import argparse
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import zipfile

TARGETS = ("linux/amd64", "linux/arm64", "windows/amd64", "darwin/amd64", "darwin/arm64")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", nargs="?", default="dev")
    parser.add_argument("--target", choices=TARGETS, action="append", help="Build only selected targets")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9._+-]+", args.version):
        parser.error("Version must contain only letters, digits, dots, underscores, + or -")

    dist = Path("dist")
    dist.mkdir(exist_ok=True)
    checksums = []
    for target in dict.fromkeys(args.target or TARGETS):
        goos, goarch = target.split("/")
        name = f"subconfig_{args.version}_{goos}_{goarch}"
        stage = dist / name
        # Rebuilding a version must not retain deleted examples or old binaries.
        if stage.exists():
            shutil.rmtree(stage)
        stage.mkdir()
        executable = "subconfig.exe" if goos == "windows" else "subconfig"
        env = dict(os.environ, CGO_ENABLED="0", GOOS=goos, GOARCH=goarch)
        subprocess.run([
            "go", "build", "-trimpath", "-ldflags", f"-s -w -X main.version={args.version}",
            "-o", str(stage / executable), "./cmd/subconfig",
        ], env=env, check=True)
        for filename in ("README.md", "LICENSE", "NOTICE", "docker-compose.yaml", ".env.example"):
            shutil.copy2(filename, stage)
        shutil.copytree("examples", stage / "examples")

        if goos == "windows":
            archive = dist / f"{name}.zip"
            with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as output:
                for path in sorted(stage.rglob("*")):
                    if path.is_file():
                        output.write(path, path.relative_to(dist))
        else:
            archive = dist / f"{name}.tar.gz"
            with tarfile.open(archive, "w:gz") as output:
                output.add(stage, arcname=name)
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        checksums.append(f"{digest}  {archive.name}\n")
        print(f"Created {archive}", flush=True)

    (dist / "SHA256SUMS").write_text("".join(checksums), encoding="utf-8")
    print("Created dist/SHA256SUMS (archives built in this invocation)")


if __name__ == "__main__":
    main()
