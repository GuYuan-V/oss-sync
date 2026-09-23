#!/usr/bin/env bash
set -Eeuo pipefail

fail() { printf '[OSS] 错误: %s\n' "$*" >&2; exit 1; }
info() { printf '[OSS] %s\n' "$*"; }
prompt() {
  local value=""
  if [[ -r /dev/tty ]] && { exec 3</dev/tty; } 2>/dev/null; then
    read -r -p "$1" value <&3 || true
    exec 3<&-
  fi
  printf '%s' "$value"
}
valid_path() { [[ "$1" =~ ^/[a-zA-Z0-9_./-]+$ && "$1" != / && "$1" != *'/../'* && "$1" != */.. ]]; }

require_runtime() {
  [[ "$(uname -s)" == Linux ]] || fail '仅支持 Linux + systemd'
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) fail '该架构暂无官方二进制，请参照部署文档手动构建；不会自动安装工具链' ;;
  esac
  [[ "$(id -u)" == 0 ]] || fail '请使用 sudo bash oss.sh 运行'
  for tool in systemctl curl python3 sha256sum flock install getent useradd timeout realpath find stat; do
    command -v "$tool" >/dev/null || fail "缺少必要工具：$tool，请安装后重试"
  done
  [[ -d /run/systemd/system ]] || fail '当前环境没有运行 systemd'
}

deploy() {
  INSTALL_DIR="${OSS_INSTALL_DIR:-}"
  [[ -n "$INSTALL_DIR" ]] || INSTALL_DIR="$(prompt '部署目录 [/opt/oss-sync]：')"
  INSTALL_DIR="${INSTALL_DIR:-/opt/oss-sync}"
  valid_path "$INSTALL_DIR" || fail '部署目录必须为不含空格或特殊字符的绝对路径'
  [[ ! -L "$INSTALL_DIR" ]] || fail '部署目录不能为符号链接'
  INSTALL_DIR="$(realpath -m "$INSTALL_DIR")"
  [[ "$INSTALL_DIR" != / ]] || fail '不能部署到根目录'
  case "$INSTALL_DIR/" in /home/*|/root/*|/run/user/*) fail 'systemd ProtectHome 不允许在用户家目录部署' ;; esac
  SERVICE=oss-sync
  UNIT=/etc/systemd/system/oss-sync.service
  GLOBAL_BIN_DIR="${OSS_GLOBAL_BIN_DIR:-/usr/local/bin}"
  valid_path "$GLOBAL_BIN_DIR" || fail '全局命令目录必须是有效绝对路径'
  exec 9>/run/lock/oss-sync-deploy.lock
  flock -n 9 || fail '已有部署或管理操作正在运行'
  if [[ -e "$UNIT" ]] && { ! grep -Fxq "WorkingDirectory=$INSTALL_DIR" "$UNIT" || [[ ! -f "$INSTALL_DIR/deployment.env" ]]; }; then
    fail '已存在其他部署的 oss-sync.service，请使用原部署目录'
  fi
  if [[ ! -f "$INSTALL_DIR/.binary-deployment" && ! -f "$INSTALL_DIR/deployment.env" && -d "$INSTALL_DIR/data" ]]; then
    fail '发现已有数据目录，请先按部署文档迁移，不会自动接管或修改旧数据'
  fi
  if [[ -d "$INSTALL_DIR" ]]; then
    [[ ! -L "$INSTALL_DIR" && "$(stat -c %u "$INSTALL_DIR")" == 0 ]] || fail '已有部署目录必须属于 root 且不是符号链接'
    [[ -z "$(find "$INSTALL_DIR" -maxdepth 1 -type l -print -quit)" ]] || fail '部署目录顶层不能包含符号链接'
  fi
  PORT="${OSS_PORT:-}"
  LIMIT="${OSS_STORAGE_LIMIT_GB:-}"
  SOURCE="${OSS_RELEASE_PROXY:-}"
  VERSION="${OSS_VERSION:-}"
  BASE="${OSS_RELEASE_BASE_URL:-}"
  MODE="${1:-install}"
  [[ "$MODE" == install || "$MODE" == configure ]] || fail '不支持的操作'
  if [[ -f "$INSTALL_DIR/deployment.env" ]]; then
    [[ "$(stat -c %u "$INSTALL_DIR/deployment.env")" == 0 ]] || fail '部署元数据必须属于 root'
    # 部署元数据由 root 生成，不能由服务账户修改
    source "$INSTALL_DIR/deployment.env"
    PORT="${PORT:-$DEPLOY_PORT}"
    LIMIT="${LIMIT:-$DEPLOY_LIMIT}"
    SOURCE="${SOURCE:-$DEPLOY_PROXY}"
    GLOBAL_BIN_DIR="$DEPLOY_BIN_DIR"
  fi
  [[ "$MODE" != configure || -f "$INSTALL_DIR/deployment.env" ]] || fail '尚未安装，不能修改配置'
  [[ -n "$PORT" ]] || PORT="$(prompt '服务端口 [8080]：')"
  PORT="${PORT:-8080}"
  [[ "$PORT" =~ ^[1-9][0-9]{0,4}$ ]] && ((PORT <= 65535)) || fail '端口必须为 1-65535'
  [[ -n "$LIMIT" ]] || LIMIT="$(prompt '存储上限 GiB，0 表示不限 [0]：')"
  LIMIT="${LIMIT:-0}"
  [[ "$LIMIT" =~ ^(0|[1-9][0-9]{0,3})$ ]] && ((LIMIT <= 4096)) || fail '存储上限必须为 0-4096 GiB'
  if [[ -z "$SOURCE" ]]; then
    choice="$(prompt '更新源：1 默认加速；2 官方；3 自定义 [1]：')"
    case "$choice" in
      ''|1) SOURCE=https://gh-proxy.com/ ;;
      2) SOURCE=official ;;
      3) SOURCE="$(prompt 'HTTPS 加速前缀：')" ;;
      *) fail '无效更新源' ;;
    esac
  fi
  python3 - "$SOURCE" <<'PY'
import sys, urllib.parse
s=sys.argv[1]
if s == 'official': sys.exit(0)
try:
    u=urllib.parse.urlsplit(s)
    port=u.port
except ValueError:
    sys.exit('无效 HTTPS 加速前缀')
if len(s)>1024 or u.scheme!='https' or not u.hostname or u.username is not None or '?' in s or '#' in s or any(c.isspace() or ord(c)<32 or c=='\\' for c in s):
    sys.exit('无效 HTTPS 加速前缀')
PY

  TEMP="$(mktemp -d)"
  CHANGED=0
  HAD_INSTALL=0
  WAS_ACTIVE=0
  WAS_ENABLED=0
  rollback() {
    local code=$? restore_failed=0
    trap - EXIT
    set +e
    if ((CHANGED)); then
      info '部署失败，恢复先前程序和配置；数据库不自动回退'
      systemctl stop "$SERVICE" || true
      if ((HAD_INSTALL)); then
        for path in bin/oss-server configs/config.prod.yaml configs/config.prod.yaml.dist VERSION service.env deployment.env oss.sh; do
          if [[ -f "$TEMP/backup/$path" ]]; then cp -p "$TEMP/backup/$path" "$INSTALL_DIR/$path" || restore_failed=1; else rm -f "$INSTALL_DIR/$path" || restore_failed=1; fi
        done
        if [[ -f "$TEMP/unit" ]]; then cp -p "$TEMP/unit" "$UNIT" || restore_failed=1; else rm -f "$UNIT"; fi
        systemctl daemon-reload || true
        if ((WAS_ENABLED)); then systemctl enable "$SERVICE" || true; else systemctl disable "$SERVICE" || true; fi
        if ((WAS_ACTIVE)); then systemctl start "$SERVICE" || true; fi
      else
        systemctl disable "$SERVICE" || true
        rm -f "$UNIT" "$INSTALL_DIR/deployment.env"
        systemctl daemon-reload || true
      fi
    fi
    if ((restore_failed)); then
      info "部分文件恢复失败，备份保留在 $TEMP/backup，请人工恢复"
    else
      rm -rf "$TEMP"
    fi
    exit "$code"
  }
  trap rollback EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  fetch() {
    local url="$1"
    [[ "$SOURCE" == official ]] || url="${SOURCE%/}/$url"
    curl --fail --location --retry 3 --connect-timeout 15 --max-time 300 "$url" -o "$2"
  }
  verify() {
    local name="$1" hash
    hash="$(awk -v n="$name" '$2==n || $2=="*"n {print $1}' "$TEMP/checksums.txt")"
    [[ "$hash" =~ ^[a-fA-F0-9]{64}$ ]] || fail "校验文件缺少唯一 SHA-256：$name"
    printf '%s  %s\n' "$hash" "$TEMP/$name" | sha256sum -c - >/dev/null || fail "SHA-256 不匹配：$name"
  }
  if [[ "$MODE" == install ]]; then
    if [[ -z "$BASE" ]]; then
      if [[ -z "$VERSION" ]]; then
        fetch https://api.github.com/repos/helantianshen/oss-sync/releases/latest "$TEMP/release.json"
        TAG="$(python3 - "$TEMP/release.json" <<'PY'
import json,sys
print(json.load(open(sys.argv[1]))['tag_name'])
PY
  )"
      else
        TAG="$VERSION"
      fi
      [[ "$TAG" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]] || fail '不支持的 Release 标签'
      VERSION="${TAG#v}"
      BASE="https://github.com/helantianshen/oss-sync/releases/download/$TAG"
    else
      [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]] || fail '自定义 Release 基地址必须同时提供精确 OSS_VERSION'
      [[ "$BASE" == https://* || "$BASE" == file://* ]] || fail 'Release 基地址必须为 HTTPS 或本地 file URL'
    fi
    ASSET="oss-sync_${VERSION}_linux_${ARCH}.tar.gz"
    fetch "${BASE%/}/checksums.txt" "$TEMP/checksums.txt"
    for name in "$ASSET" oss.sh; do
      fetch "${BASE%/}/$name" "$TEMP/$name"
      verify "$name"
    done
    python3 - "$TEMP/$ASSET" "$TEMP" "$VERSION" <<'PYPACK'
import sys,tarfile,shutil
from pathlib import Path
archive,dest,version=sys.argv[1:]
expected={'bin/oss-server':'oss-server','configs/config.prod.yaml':'config.prod.yaml','VERSION':'VERSION'}
with tarfile.open(archive) as t:
    members=t.getmembers()
    seen=set()
    for m in members:
        if m.isdir() and m.name in ('bin','configs'):
            continue
        if m.name not in expected or not m.isfile() or m.name in seen or m.size<=0 or m.size>256*1024*1024:
            sys.exit('运行包结构无效：仅接受二进制、生产配置和 VERSION 普通文件')
        seen.add(m.name)
    if seen!=set(expected): sys.exit('运行包缺少必要文件')
    for name,output in expected.items():
        with t.extractfile(name) as src, open(Path(dest)/output,'wb') as dst:
            shutil.copyfileobj(src,dst)
if (Path(dest)/'VERSION').read_text().strip()!=version:
    sys.exit('运行包 VERSION 与发布版本不匹配')
PYPACK
    python3 - "$TEMP/oss-server" <<'PYELF'
import sys
with open(sys.argv[1], 'rb') as f:
    if f.read(4) != b'\x7fELF': sys.exit('下载文件不是 ELF 可执行程序')
PYELF
    chmod 755 "$TEMP/oss-server"
    [[ "$(timeout 15 "$TEMP/oss-server" --version)" == "$VERSION" ]] || fail '程序版本不匹配'
    bash -n "$TEMP/oss.sh"
  else
    VERSION="$("$INSTALL_DIR/bin/oss-server" --version)"
  fi

  for name in oss oss-sync; do
    if [[ -e "$GLOBAL_BIN_DIR/$name" || -L "$GLOBAL_BIN_DIR/$name" ]]; then
      [[ "$(readlink -f "$GLOBAL_BIN_DIR/$name")" == "$INSTALL_DIR/oss.sh" ]] || fail "命令已被其他程序占用：$name"
    fi
  done
  if [[ -f "$INSTALL_DIR/deployment.env" ]]; then
    HAD_INSTALL=1
    mkdir -p "$TEMP/backup"
    for path in bin/oss-server configs/config.prod.yaml configs/config.prod.yaml.dist VERSION service.env deployment.env oss.sh; do
      mkdir -p "$TEMP/backup/$(dirname "$path")"
      if [[ -f "$INSTALL_DIR/$path" ]]; then cp -p "$INSTALL_DIR/$path" "$TEMP/backup/$path"; fi
    done
    if [[ -f "$UNIT" ]]; then cp -p "$UNIT" "$TEMP/unit"; fi
    systemctl is-active --quiet "$SERVICE" && WAS_ACTIVE=1
    systemctl is-enabled --quiet "$SERVICE" && WAS_ENABLED=1
  fi
  if ! getent passwd oss-sync >/dev/null; then
    useradd --system --user-group --home-dir "$INSTALL_DIR" --no-create-home --shell /usr/sbin/nologin oss-sync
  fi
  getent group oss-sync >/dev/null || fail '缺少 oss-sync 服务组'
  [[ "$(id -u oss-sync)" != 0 ]] || fail '服务账户不能为 root'
  install -d -o root -g root -m 755 "$INSTALL_DIR" "$INSTALL_DIR/bin" "$INSTALL_DIR/configs" "$GLOBAL_BIN_DIR"
  install -d -o oss-sync -g oss-sync -m 750 "$INSTALL_DIR/data"
  touch "$INSTALL_DIR/.binary-deployment"
  chmod 600 "$INSTALL_DIR/.binary-deployment"
  systemctl stop "$SERVICE" 2>/dev/null || { ((HAD_INSTALL == 0)) || fail '无法停止旧服务'; }
  CHANGED=1
  if [[ "$MODE" == install ]]; then
    install -m 755 "$TEMP/oss-server" "$INSTALL_DIR/bin/oss-server.new"
    mv -f "$INSTALL_DIR/bin/oss-server.new" "$INSTALL_DIR/bin/oss-server"
    install -m 755 "$TEMP/oss.sh" "$INSTALL_DIR/oss.sh.new"
    mv -f "$INSTALL_DIR/oss.sh.new" "$INSTALL_DIR/oss.sh"
    if [[ ! -f "$INSTALL_DIR/configs/config.prod.yaml" ]]; then
      install -o root -g oss-sync -m 640 "$TEMP/config.prod.yaml" "$INSTALL_DIR/configs/config.prod.yaml"
    fi
    install -o root -g oss-sync -m 640 "$TEMP/config.prod.yaml" "$INSTALL_DIR/configs/config.prod.yaml.dist"
    install -m 644 "$TEMP/VERSION" "$INSTALL_DIR/VERSION"
  fi
  cat > "$INSTALL_DIR/service.env" <<ENV
OSS_ENV=prod
OSS_SERVER_PORT=$PORT
OSS_STORAGE_MAX_TOTAL_SIZE_MB=$((LIMIT * 1024))
OSS_UPDATE_MANAGER=systemd
ENV
  chmod 640 "$INSTALL_DIR/service.env"
  chown root:oss-sync "$INSTALL_DIR/service.env"
  cat > "$UNIT" <<UNIT
[Unit]
Description=OSS Sync server
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=oss-sync
Group=oss-sync
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$INSTALL_DIR/service.env
ExecStart=$INSTALL_DIR/bin/oss-server
Restart=on-failure
RestartSec=3
TimeoutStopSec=30
UMask=0027
NoNewPrivileges=true
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
ProtectSystem=full
ProtectHome=true
ReadWritePaths=$INSTALL_DIR/data

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable "$SERVICE"
  systemctl start "$SERVICE"
  healthy=0
  for ((attempt=0; attempt<60; attempt++)); do
    if systemctl is-active --quiet "$SERVICE" && curl -fsS --max-time 2 "http://127.0.0.1:$PORT/readyz" > "$TEMP/ready.json"; then
      if python3 - "$TEMP/ready.json" "$VERSION" <<'PY'
import json,sys
p=json.load(open(sys.argv[1]))
sys.exit(0 if p.get('ready') is True and p.get('version')==sys.argv[2] else 1)
PY
      then healthy=1; break; fi
    fi
    sleep 1
  done
  ((healthy)) || fail '新版本未通过健康/版本检查，请查看 journalctl -u oss-sync'
  {
    printf 'DEPLOY_PORT=%q\n' "$PORT"
    printf 'DEPLOY_LIMIT=%q\n' "$LIMIT"
    printf 'DEPLOY_PROXY=%q\n' "$SOURCE"
    printf 'DEPLOY_BIN_DIR=%q\n' "$GLOBAL_BIN_DIR"
    printf 'DEPLOY_VERSION=%q\n' "$VERSION"
  } > "$INSTALL_DIR/deployment.env"
  chmod 600 "$INSTALL_DIR/deployment.env"
  for name in oss oss-sync; do ln -sfn "$INSTALL_DIR/oss.sh" "$GLOBAL_BIN_DIR/$name"; done
  CHANGED=0
  info "部署完成：$INSTALL_DIR，版本 $VERSION，端口 $PORT"
  info '管理命令：sudo oss；日志：sudo journalctl -u oss-sync -f'
}

service_state() {
  if systemctl is-active --quiet oss-sync 2>/dev/null; then printf '运行中'; else printf '未运行'; fi
}

installed_version() {
  local version=""
  [[ -r "$DIR/VERSION" ]] && version="$(tr -d '[:space:]' < "$DIR/VERSION")"
  if [[ -z "$version" && -x "$DIR/bin/oss-server" ]]; then
    version="$("$DIR/bin/oss-server" --version 2>/dev/null || true)"
  fi
  printf '%s' "${version:-未知}"
}

service_url() {
  local ip port
  port="${DEPLOY_PORT:-8080}"
  ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  [[ -n "$ip" ]] || ip="$(ip -4 route get 1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}')"
  [[ -n "$ip" ]] || ip="127.0.0.1"
  printf 'http://%s:%s' "$ip" "$port"
}

print_header() {
  local used_bytes limit_gb
  used_bytes="$(du -sb "$DIR/data" 2>/dev/null | awk '{print $1}')"
  [[ "$used_bytes" =~ ^[0-9]+$ ]] || used_bytes=0
  limit_gb="${DEPLOY_LIMIT:-0}"
  python3 - "$(installed_version)" "$(service_state)" "$(service_url)" "$used_bytes" "$limit_gb" <<'PY'
import sys, unicodedata
version, state, url, used, limit = sys.argv[1:6]
used, limit = int(used), int(limit)

def fmt(size):
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if size < 1024 or unit == "TB":
            return f"{size} B" if unit == "B" else f"{size:.1f} {unit}"
        size /= 1024

storage = f"存储：{fmt(used)} / {limit} GiB" if limit > 0 else f"存储：{fmt(used)} / 不限"
lines = [f"OSS Sync {version}", f"状态：{state}", f"地址：{url}", storage]

def width(text):
    return sum(2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1 for ch in text)

inner = max(width(line) for line in lines)
rule = "─" * (inner + 2)
print(f"┌{rule}┐")
for line in lines:
    print(f"│ {line}{' ' * (inner - width(line))} │")
print(f"└{rule}┘")
PY
}

print_menu() {
  local state
  state="$(service_state)"
  print_header
  printf '\n'
  if [[ "$state" == 运行中 ]]; then
    printf '1 更新\n2 停止\n3 重启\n4 修改\n5 日志\n6 卸载\n0 退出\n'
  else
    printf '1 更新\n2 启动\n3 重启\n4 修改\n5 日志\n6 卸载\n0 退出\n'
  fi
}

acquire_lock() {
  exec 9>/run/lock/oss-sync-deploy.lock
  flock -n 9 || fail '已有部署操作正在运行'
}

manage_update() {
  local source_url="${OSS_RELEASE_PROXY:-}"
  if [[ -n "$source_url" ]]; then
    acquire_lock
    OSS_INSTALL_DIR="$DIR" OSS_RELEASE_PROXY="$source_url" deploy install
    return 0
  fi
  printf '1 默认加速\n2 官方\n3 自定义\n0 返回\n'
  local selection
  selection="$(prompt '更新源 [1]：')"
  case "$selection" in
    0) return 0 ;;
    ''|1|proxy) source_url=https://gh-proxy.com/ ;;
    2|official) source_url=official ;;
    3|custom)
      source_url="$(prompt 'HTTPS 前缀：')"
      if [[ -z "$source_url" ]]; then
        info '已取消更新'
        return 0
      fi
      ;;
    https://*) source_url="$selection" ;;
    *) fail '无效更新源' ;;
  esac
  acquire_lock
  OSS_INSTALL_DIR="$DIR" OSS_RELEASE_PROXY="$source_url" deploy install
}

manage_modify() {
  local choice value
  choice="${1:-}"
  value="${2:-}"
  if [[ -z "$choice" ]]; then
    printf '1 修改容量\n2 修改端口\n0 返回\n'
    choice="$(prompt '选择 [0]：')"
    choice="${choice:-0}"
  fi
  case "$choice" in
    1|storage)
      acquire_lock
      [[ -n "$value" ]] || value="$(prompt "容量上限 GiB（当前 ${DEPLOY_LIMIT:-0}，0 不限）：")"
      OSS_INSTALL_DIR="$DIR" OSS_STORAGE_LIMIT_GB="$value" deploy configure
      ;;
    2|port)
      acquire_lock
      [[ -n "$value" ]] || value="$(prompt "新端口（当前 ${DEPLOY_PORT:-8080}：")"
      OSS_INSTALL_DIR="$DIR" OSS_PORT="$value" deploy configure
      ;;
    0) return 0 ;;
    *) fail '无效操作' ;;
  esac
}

manage_uninstall() {
  printf '1 卸载全部（同时删除数据）\n2 保留数据\n0 返回\n'
  local choice answer
  choice="${1:-}"
  answer="${2:-}"
  if [[ -z "$choice" ]]; then
    choice="$(prompt '选择 [0]：')"
    choice="${choice:-0}"
  fi
  case "$choice" in
    1|all)
      [[ -n "$answer" ]] || answer="$(prompt '将删除全部数据且无法恢复，确认卸载全部？[y/N]：')"
      [[ "$answer" == y || "$answer" == yes ]] || return 0
      acquire_lock
      systemctl disable --now oss-sync
      rm -f /etc/systemd/system/oss-sync.service
      systemctl daemon-reload
      for name in oss oss-sync; do
        [[ "$(readlink -f "$DEPLOY_BIN_DIR/$name")" != "$DIR/oss.sh" ]] || rm -f "$DEPLOY_BIN_DIR/$name"
      done
      printf '服务已卸载，正在删除 %s ...\n' "$DIR"
      rm -rf "$DIR"
      printf '已全部卸载，数据已删除\n'
      MENU_EXIT=1
      ;;
    2|keep)
      [[ -n "$answer" ]] || answer="$(prompt '保留数据，仅卸载服务？[y/N]：')"
      [[ "$answer" == y || "$answer" == yes ]] || return 0
      acquire_lock
      systemctl disable --now oss-sync
      rm -f /etc/systemd/system/oss-sync.service
      systemctl daemon-reload
      for name in oss oss-sync; do
        [[ "$(readlink -f "$DEPLOY_BIN_DIR/$name")" != "$DIR/oss.sh" ]] || rm -f "$DEPLOY_BIN_DIR/$name"
      done
      printf '服务已卸载；配置、程序、服务账户和数据保留在 %s\n' "$DIR"
      MENU_EXIT=1
      ;;
    0) return 0 ;;
    *) fail '无效操作' ;;
  esac
}

manage() {
  local choice="${1:-}"
  MENU_EXIT=0
  while :; do
    if [[ -z "$choice" ]]; then
      print_menu
      choice="$(prompt '选择 [0]：')"
      choice="${choice:-0}"
    fi
    case "$choice" in
      0) return 0 ;;
      1|update) manage_update ;;
      2|start|stop)
        acquire_lock
        if [[ "$(service_state)" == 运行中 ]]; then
          systemctl stop oss-sync
          info '服务已停止'
        else
          systemctl start oss-sync
          info '服务已启动'
        fi
        ;;
      3|restart)
        acquire_lock
        systemctl restart oss-sync
        info '服务已重启'
        ;;
      4|modify) manage_modify "${2:-}" "${3:-}" ;;
      5|logs) journalctl -u oss-sync -f ;;
      6|uninstall) manage_uninstall "${2:-}" "${3:-}" ;;
      status) systemctl status oss-sync --no-pager ;;
      *) fail '无效操作' ;;
    esac
    # 命令行一次性调用执行完即退出；交互模式返回主菜单。
    if [[ -n "${1:-}" ]]; then return 0; fi
    if ((MENU_EXIT)); then return 0; fi
    choice=""
  done
}

main() {
  require_runtime
  SELF=""
  if [[ -f "${BASH_SOURCE[0]:-}" ]]; then SELF="$(readlink -f "${BASH_SOURCE[0]}")"; fi
  DIR="${OSS_INSTALL_DIR:-}"
  if [[ -z "$DIR" && -n "$SELF" && -f "$(dirname "$SELF")/deployment.env" ]]; then
    DIR="$(dirname "$SELF")"
  fi
  if [[ -z "$DIR" && -f /etc/systemd/system/oss-sync.service ]]; then
    DIR="$(sed -n 's/^WorkingDirectory=//p' /etc/systemd/system/oss-sync.service)"
  fi
  DIR="${DIR:-/opt/oss-sync}"
  if [[ "${1:-}" == install || ! -f "$DIR/deployment.env" ]]; then
    [[ -z "${1:-}" || "$1" == install ]] || fail '尚未安装，请先运行 sudo bash oss.sh install'
    if [[ -f "$DIR/deployment.env" ]]; then OSS_INSTALL_DIR="$DIR" deploy install; else deploy install; fi
    return
  fi
  [[ "$(stat -c %u "$DIR/deployment.env")" == 0 ]] || fail '部署元数据必须属于 root'
  source "$DIR/deployment.env"
  manage "$@"
}

main "$@"
