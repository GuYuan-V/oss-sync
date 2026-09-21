#!/usr/bin/env python3
"""将已编译程序与默认配置组成平台运行包"""
import argparse
from pathlib import Path
import re
import tarfile
import tempfile
import shutil
import zipfile


def package(binary: Path, config: Path, version: str, goos: str, arch: str, output: Path) -> Path:
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-rc\.\d+)?", version):
        raise ValueError("invalid release version")
    if (goos, arch) not in {("linux", "amd64"), ("linux", "arm64"), ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64")}:
        raise ValueError("unsupported platform")
    output.mkdir(parents=True, exist_ok=True)
    extension = ".zip" if goos == "windows" else ".tar.gz"
    target = output / f"oss-sync_{version}_{goos}_{arch}{extension}"
    with tempfile.TemporaryDirectory(prefix="oss-package-") as directory:
        root = Path(directory)
        name = "bin/oss-server.exe" if goos == "windows" else "bin/oss-server"
        files = [name, "configs/config.prod.yaml", "VERSION"]
        (root / "bin").mkdir()
        (root / "configs").mkdir()
        shutil.copyfile(binary, root / name)
        (root / name).chmod(0o755)
        shutil.copyfile(config, root / files[1])
        (root / files[1]).chmod(0o644)
        (root / "VERSION").write_text(version + "\n", encoding="utf-8")
        (root / "VERSION").chmod(0o644)
        if extension == ".zip":
            with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as archive:
                for entry in files:
                    archive.write(root / entry, entry)
        else:
            with tarfile.open(target, "w:gz") as archive:
                for entry in files:
                    archive.add(root / entry, arcname=entry, recursive=False)
    return target


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--config", type=Path, default=Path("configs/config.prod.yaml"))
    parser.add_argument("--version", required=True)
    parser.add_argument("--os", dest="goos", required=True)
    parser.add_argument("--arch", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    print(package(args.binary, args.config, args.version, args.goos, args.arch, args.output))
