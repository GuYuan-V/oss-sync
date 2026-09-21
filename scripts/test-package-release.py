#!/usr/bin/env python3
"""验证所有发布平台的运行包布局与文件内容"""
import importlib.util
import sys
from pathlib import Path
import tarfile
import tempfile
import zipfile

sys.dont_write_bytecode = True

spec = importlib.util.spec_from_file_location("package_release", Path(__file__).with_name("package-release.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    binary, config = root / "binary", root / "config"
    binary.write_bytes(b"platform binary")
    config.write_text("server:\n  port: 8080\n")
    for goos, arch in [("linux", "amd64"), ("linux", "arm64"), ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64")]:
        target = module.package(binary, config, "1.2.3", goos, arch, root / "dist")
        executable = "bin/oss-server.exe" if goos == "windows" else "bin/oss-server"
        if goos == "windows":
            with zipfile.ZipFile(target) as archive:
                contents = {name: archive.read(name) for name in archive.namelist()}
        else:
            with tarfile.open(target) as archive:
                contents = {entry.name: archive.extractfile(entry).read() for entry in archive.getmembers()}
                assert archive.getmember(executable).mode == 0o755
        assert set(contents) == {executable, "configs/config.prod.yaml", "VERSION"}
        assert contents[executable] == binary.read_bytes()
        assert contents["configs/config.prod.yaml"] == config.read_bytes()
        assert contents["VERSION"] == b"1.2.3\n"
print("PASS: all five platform packages contain binary/config/VERSION, without scripts or data")
