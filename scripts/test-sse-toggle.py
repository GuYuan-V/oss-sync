#!/usr/bin/env python3
"""验证明文 SSE 开关的配置边界、原子更新和锁释放"""
import fcntl
from pathlib import Path
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix='oss-sse-test-') as directory:
    root = Path(directory)
    lock = root / 'lock'
    script = root / 'functions.sh'
    script.write_text((repo / 'oss.sh').read_text().rsplit('\nmain "$@"', 1)[0].replace('/run/lock/oss-sync-deploy.lock', str(lock)))
    config = root / 'configs/config.prod.yaml'
    config.parent.mkdir()
    def run(body, *args, ok=True):
        result = subprocess.run(['bash', '-c', 'set -Eeuo pipefail; source "$1"; shift; ' + body, 'test', str(script), *map(str, args)], text=True, capture_output=True)
        assert (result.returncode == 0) == ok, result.stderr
        return result.stdout.strip()
    config.write_text('server:\n  allow_insecure_query_token: true\nsync:\n  allow_insecure_query_token: true # 保留说明\n  max_concurrency: 6\nupdate:\n  github_repo: example/repo\n')
    config.chmod(0o640)
    original = config.read_text()
    metadata = config.stat()
    assert run('insecure_query_token_value "$1"', config) == 'true'
    run('insecure_query_token_value "$1" false', config)
    assert config.read_text() == original.replace('true # 保留说明', 'false # 保留说明')
    assert config.stat().st_mode == metadata.st_mode
    assert (config.stat().st_uid, config.stat().st_gid) == (metadata.st_uid, metadata.st_gid)
    run('DIR="$1"; prompt() { printf y; }; toggle_insecure_query_token; acquire_lock', root)
    assert run('insecure_query_token_value "$1"', config) == 'true'
    run('DIR="$1"; prompt() { printf n; }; toggle_insecure_query_token; acquire_lock', root)
    assert run('insecure_query_token_value "$1"', config) == 'true'
    config.write_text('sync:\n  max_concurrency: 6\nupdate:\n  github_repo: example/repo\n')
    assert run('insecure_query_token_value "$1"', config) == 'false'
    run('insecure_query_token_value "$1" true', config)
    assert 'sync:\n  allow_insecure_query_token: true\n' in config.read_text()
    config.write_text('sync:\n  extension:\n    allow_insecure_query_token: true\n')
    run('insecure_query_token_value "$1" false', config)
    assert config.read_text() == 'sync:\n  allow_insecure_query_token: false\n  extension:\n    allow_insecure_query_token: true\n'
    with lock.open('w') as holder:
        fcntl.flock(holder, fcntl.LOCK_EX | fcntl.LOCK_NB)
        inode = lock.stat().st_ino
        run('acquire_lock', ok=False)
        assert lock.stat().st_ino == inode
    for invalid in ['server:\n  port: 8080\n', 'sync:\n  allow_insecure_query_token: true\n  allow_insecure_query_token: false\n', 'sync:\n  allow_insecure_query_token: invalid\n']:
        config.write_text(invalid)
        run('insecure_query_token_value "$1" true', config, ok=False)
        assert config.read_text() == invalid
print('PASS: inline comments, section scope, missing key, invalid config, permissions, confirmation and lock release')
