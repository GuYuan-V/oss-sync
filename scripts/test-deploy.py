#!/usr/bin/env python3
"""在临时目录中验证部署流程，systemd 和账户操作由本地替身执行"""
import fcntl
import hashlib
import io
import os
from pathlib import Path
import socket
import subprocess
import tarfile
import tempfile

REPO = Path(__file__).resolve().parents[1]
MOCK = r'''#!/usr/bin/env python3
import os, sys, subprocess, signal, json
from pathlib import Path
root=Path(os.environ['DEPLOY_TEST_ROOT'])
name=Path(sys.argv[0]).name
args=sys.argv[1:]
if name=='uname' and args==['-s'] and os.environ.get('DEPLOY_TEST_SYSTEM'):
    print(os.environ['DEPLOY_TEST_SYSTEM']); sys.exit()
if name=='uname' and args==['-m'] and os.environ.get('DEPLOY_TEST_ARCH'):
    print(os.environ['DEPLOY_TEST_ARCH']); sys.exit()
if name=='curl' and '-o' in args:
    url=args[-3]
    if url.startswith('https://mirror.example/p/https://'):
        with (root/'requests').open('a') as f: f.write(url+'\n')
        url=url.removeprefix('https://mirror.example/p/')
        output=Path(args[-1])
        if url=='https://api.github.com/repos/helantianshen/oss-sync/releases/latest':
            output.write_text(json.dumps({'tag_name':'v1.2.3'})); sys.exit()
        prefix='https://github.com/helantianshen/oss-sync/releases/download/v1.2.3/'
        if not url.startswith(prefix): sys.exit('unfixed release URL: '+url)
        output.write_bytes((root/'release'/url.removeprefix(prefix)).read_bytes()); sys.exit()
if name=='id':
    print('10001' if 'oss-sync' in args else '0'); sys.exit()
if name=='stat' and args[:2]==['-c','%u']:
    print('0'); sys.exit()
if name in ('chown','useradd','getent'): sys.exit()
if name=='install':
    clean=[]; i=0
    while i<len(args):
        if args[i] in ('-o','-g'): i+=2
        else: clean.append(args[i]); i+=1
    sys.exit(subprocess.call(['/usr/bin/install',*clean]))
if name=='systemctl':
    pidfile=root/'pid'
    cmd=args[0]
    if cmd in ('stop','disable'):
        if pidfile.exists():
            try: os.kill(int(pidfile.read_text()),signal.SIGTERM)
            except ProcessLookupError: pass
            pidfile.unlink()
            import time; time.sleep(.5)
        sys.exit()
    if cmd=='start':
        if (root/'fail-start').exists():
            (root/'fail-start').unlink(); sys.exit()
        unit=(root/'system/oss-sync.service').read_text()
        vals=dict(line.split('=',1) for line in unit.splitlines() if '=' in line)
        env=os.environ.copy()
        env.update(dict(line.split('=',1) for line in Path(vals['EnvironmentFile']).read_text().splitlines()))
        log=open(root/'server.log','ab')
        p=subprocess.Popen([vals['ExecStart']],cwd=vals['WorkingDirectory'],env=env,stdout=log,stderr=log,stdin=subprocess.DEVNULL,start_new_session=True,close_fds=True)
        pidfile.write_text(str(p.pid)); sys.exit()
    if cmd=='is-active': sys.exit(0 if pidfile.exists() else 3)
    if cmd in ('enable','daemon-reload','is-enabled','status'): sys.exit()
    sys.exit('unsupported mock operation '+cmd)
os.execv('/usr/bin/'+name,[name,*args])
'''

def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]

with tempfile.TemporaryDirectory(prefix='oss-deploy-test-') as work:
    root = Path(work)
    mock = root / 'mock'
    mock.mkdir()
    for name in ['id', 'stat', 'chown', 'useradd', 'getent', 'install', 'systemctl', 'curl', 'uname']:
        p = mock / name
        p.write_text(MOCK)
        p.chmod(0o755)
    (root / 'system').mkdir()
    (root / 'run').mkdir()
    release = root / 'release'
    release.mkdir()
    for name in ['oss.sh']:
        text = (REPO / name).read_text()
        text = text.replace('/etc/systemd/system', str(root / 'system'))
        text = text.replace('/run/systemd/system', str(root / 'system'))
        text = text.replace('/run/lock/oss-sync-deploy.lock', str(root / 'run/lock'))
        (release / name).write_text(text)
    binary = root / 'oss-server'
    subprocess.run(['go', 'build', '-ldflags', '-X github.com/helantianshen/oss-sync/internal/version.Version=1.2.3', '-o', str(binary), './cmd/server'], cwd=REPO, check=True)
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}[os.uname().machine]
    asset = f'oss-sync_1.2.3_linux_{arch}.tar.gz'
    def pack(version):
        subprocess.run(['python3', str(REPO / 'scripts/package-release.py'), '--binary', str(binary), '--version', version, '--os', 'linux', '--arch', arch, '--output', str(release)], cwd=REPO, check=True, stdout=subprocess.DEVNULL)
    pack('1.2.3')
    def checksums():
        names = [p.name for p in release.glob('oss-sync_*.tar.gz')] + ['oss.sh']
        (release / 'checksums.txt').write_text(''.join(f'{hashlib.sha256((release / n).read_bytes()).hexdigest()}  {n}\n' for n in names))
    checksums()
    dest = root / 'app'
    port = free_port()
    env = os.environ | {'PATH': str(mock) + ':' + os.environ['PATH'], 'DEPLOY_TEST_ROOT': str(root), 'OSS_INSTALL_DIR': str(dest), 'OSS_GLOBAL_BIN_DIR': str(root / 'bin'), 'OSS_VERSION': '1.2.3', 'OSS_RELEASE_BASE_URL': release.as_uri(), 'OSS_RELEASE_PROXY': 'official', 'OSS_PORT': str(port), 'OSS_STORAGE_LIMIT_GB': '1'}
    def run(command, ok=True, extra=None):
        result = subprocess.run(command, cwd=REPO, env=env | (extra or {}), text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if (result.returncode == 0) != ok:
            raise AssertionError(result.stdout + '\n' + (root / 'server.log').read_text() if (root / 'server.log').exists() else result.stdout)
        return result.stdout
    try:
        run(['bash', str(release / 'oss.sh')])
        assert (dest / 'data/oss.db').exists()
        assert (root / 'bin/oss').resolve() == dest / 'oss.sh'
        config = dest / 'configs/config.prod.yaml'
        config.write_text(config.read_text() + '\n# 保留自定义配置\n')
        original = config.read_bytes()
        script = release / 'oss.sh'
        script.write_text(script.read_text() + '\n# 同版本管理脚本校验标记\n')
        checksums()
        run(['bash', str(dest / 'oss.sh'), 'install'], extra={'OSS_VERSION': '', 'OSS_RELEASE_BASE_URL': '', 'OSS_RELEASE_PROXY': 'https://mirror.example/p/'})
        requests = (root / 'requests').read_text().splitlines()
        assert len(requests) == 4
        assert sum('/releases/latest' in url for url in requests) == 1
        assert all('/releases/download/v1.2.3/' in url for url in requests[1:])
        assert config.read_bytes() == original
        assert (dest / 'oss.sh').read_bytes() == script.read_bytes()
        assert (dest / 'configs/config.prod.yaml.dist').read_bytes() == (REPO / 'configs/config.prod.yaml').read_bytes()
        run(['bash', str(dest / 'oss.sh'), 'install'], ok=False, extra={'DEPLOY_TEST_ARCH': 'riscv64'})
        run(['bash', str(dest / 'oss.sh'), 'install'], ok=False, extra={'OSS_RELEASE_PROXY': 'http://invalid.example'})
        run(['bash', str(dest / 'oss.sh'), 'install'], ok=False, extra={'DEPLOY_TEST_SYSTEM': 'Darwin'})
        original_archive = (release / asset).read_bytes()
        for bad_name in ['../oss-server', 'bin/oss-server']:
            with tarfile.open(release / asset, 'w:gz') as archive:
                content = b'not a complete package'
                member = tarfile.TarInfo(bad_name)
                member.size = len(content)
                archive.addfile(member, io.BytesIO(content))
            checksums()
            run(['bash', str(dest / 'oss.sh'), 'install'], ok=False)
            assert (root / 'pid').exists()
        (release / asset).write_bytes(original_archive)
        checksums()
        assert (root / 'pid').exists()
        run(['bash', str(dest / 'oss.sh'), '4', '1', '2'])
        assert 'OSS_STORAGE_MAX_TOTAL_SIZE_MB=2048' in (dest / 'service.env').read_text()
        new_port = free_port()
        run(['bash', str(dest / 'oss.sh'), '4', '2', str(new_port)])
        assert f'OSS_SERVER_PORT={new_port}' in (dest / 'service.env').read_text()
        with (root / 'run/lock').open('w') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            run(['bash', str(dest / 'oss.sh'), 'install'], ok=False)
            assert (root / 'pid').exists()
        previous = (dest / 'service.env').read_bytes()
        (root / 'fail-start').touch()
        run(['bash', str(dest / 'oss.sh'), '4', '2', str(free_port())], ok=False)
        assert (dest / 'service.env').read_bytes() == previous
        assert (root / 'pid').exists()
        old_binary = (dest / 'bin/oss-server').read_bytes()
        old_script = (dest / 'oss.sh').read_bytes()
        old_version = (dest / 'VERSION').read_bytes()
        old_template = (dest / 'configs/config.prod.yaml.dist').read_bytes()
        script.write_text(script.read_text() + '\n# 下一版本管理脚本校验标记\n')
        subprocess.run(['go', 'build', '-ldflags', '-X github.com/helantianshen/oss-sync/internal/version.Version=1.2.4', '-o', str(binary), './cmd/server'], cwd=REPO, check=True)
        pack('1.2.4')
        checksums()
        (root / 'fail-start').touch()
        run(['bash', str(dest / 'oss.sh'), 'install'], ok=False, extra={'OSS_VERSION': '1.2.4'})
        assert (dest / 'bin/oss-server').read_bytes() == old_binary
        assert (dest / 'oss.sh').read_bytes() == old_script
        assert (dest / 'VERSION').read_bytes() == old_version
        assert (dest / 'configs/config.prod.yaml.dist').read_bytes() == old_template
        assert (dest / 'service.env').read_bytes() == previous
        assert (root / 'pid').exists()
        with (release / asset).open('ab') as stream: stream.write(b'corrupt')
        run(['bash', str(dest / 'oss.sh'), 'install'], ok=False)
        assert (dest / 'service.env').read_bytes() == previous
        assert config.read_bytes() == original
        run(['bash', str(dest / 'oss.sh'), '6', '2', 'yes'])
        assert (dest / 'data/oss.db').exists()
        assert not (root / 'system/oss-sync.service').exists()
        checksums()
        run(['bash', str(dest / 'oss.sh'), 'install'])
        print('PASS: install, pinned/proxied update, config preservation, port/capacity, lock, config/binary rollback, digest/platform rejection, uninstall/reinstall')
    finally:
        subprocess.run([str(mock / 'systemctl'), 'stop', 'oss-sync'], env=env, check=False)
