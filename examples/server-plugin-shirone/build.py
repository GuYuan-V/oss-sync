#!/usr/bin/env python3
"""使用 Python 标准库生成 Shirone 插件资源包"""

import argparse
import hashlib
import json
from pathlib import Path
import zipfile


SOURCE = Path(__file__).resolve().parent


def leb(value, signed=False):
    result = bytearray()
    while True:
        part = value & 127
        value >>= 7
        done = (value == 0 and (not signed or not part & 64)) or (signed and value == -1 and part & 64)
        result.append(part | (0 if done else 128))
        if done:
            return bytes(result)


def settings_module():
    # ABI v1 设置插件不注册路由和 Hook，表单与配置读取由宿主完成
    def section(number, body):
        return bytes([number]) + leb(len(body)) + body

    def export(name, kind, index):
        return leb(len(name)) + name.encode() + bytes([kind, index])

    def function(code):
        body = b"\x00" + code + b"\x0b"
        return leb(len(body)) + body

    response = b'{"status":204}'
    exports = b"\x04" + export("memory", 2, 0) + export("oss_abi_version", 0, 0)
    exports += export("oss_alloc", 0, 1) + export("oss_handle", 0, 2)
    code = b"\x03" + function(b"\x41\x01") + function(b"\x41" + leb(4096, True))
    code += function(b"\x42" + leb((128 << 32) | len(response), True))
    data = b"\x01\x00\x41" + leb(128, True) + b"\x0b" + leb(len(response)) + response
    # 请求最多 1 MiB，17 页内存容纳从 4096 开始的请求，响应保留在 128
    sections = [(1, bytes([3, 96, 0, 1, 127, 96, 1, 127, 1, 127, 96, 2, 127, 127, 1, 126])),
                (3, b"\x03\x00\x01\x02"), (5, b"\x01\x01\x11\x11"),
                (7, exports), (10, code), (11, data)]
    return b"\x00asm\x01\x00\x00\x00" + b"".join(section(*entry) for entry in sections)


def package(output):
    manifest = json.loads((SOURCE / "manifest.json").read_text(encoding="utf-8"))
    files = {"manifest.json": (SOURCE / "manifest.json").read_bytes(), "plugin.wasm": settings_module()}
    for resource in manifest["blog_themes"]:
        for path in sorted((SOURCE / resource["path"]).rglob("*")):
            if path.is_symlink():
                raise ValueError(f"资源不能包含符号链接: {path}")
            if path.is_file():
                files[path.relative_to(SOURCE).as_posix()] = path.read_bytes()
        if resource["path"] + "/template.html" not in files:
            raise ValueError("缺少模板入口")
    if len(files) > 512 or sum(map(len, files.values())) > 64 << 20:
        raise ValueError("资源数量或解压大小超过插件限制")
    if len(files["manifest.json"]) > 64 << 10 or len(files["plugin.wasm"]) > 8 << 20:
        raise ValueError("插件清单或运行入口超过大小限制")
    if any(len(data) > 32 << 20 for data in files.values()):
        raise ValueError("插件资源超过单文件限制")
    output.mkdir(parents=True, exist_ok=True)
    target = output / "shirone.zip"
    with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            archive.writestr(info, data)
    if target.stat().st_size > 32 << 20:
        target.unlink()
        raise ValueError("ZIP 超过上传大小限制")
    checksum = hashlib.sha256(target.read_bytes()).hexdigest()
    target.with_suffix(".zip.sha256").write_text(f"{checksum}  {target.name}\n", encoding="utf-8")
    print(f"{target}\n{len(files)} files, {target.stat().st_size} bytes\nSHA-256: {checksum}")
    return target


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=SOURCE.parents[1] / "bin/themes")
    package(parser.parse_args().output)
