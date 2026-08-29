#!/usr/bin/env bash

set -Eeuo pipefail

IMAGE="${OSS_IMAGE:-ghcr.io/helantianshen/oss-sync-server:latest}"
CONTAINER="${OSS_CONTAINER_NAME:-oss-sync}"
VOLUME="${OSS_DATA_VOLUME:-oss-data}"
PORT="${OSS_PORT:-8080}"
BIND_ADDRESS="${OSS_BIND_ADDRESS:-0.0.0.0}"
INSTALL_DOCKER="${OSS_INSTALL_DOCKER:-}"
SKIP_PULL="${OSS_SKIP_PULL:-0}"

info() { printf '[OSS] %s\n' "$*"; }
fail() { printf '[OSS] 错误: %s\n' "$*" >&2; exit 1; }

run_privileged() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    fail "此操作需要 root 权限，请使用 sudo 重新运行"
  fi
}

confirm_docker_install() {
  case "${INSTALL_DOCKER,,}" in
    1|true|y|yes) return 0 ;;
    0|false|n|no) return 1 ;;
  esac
  local answer=""
  if ! { exec 3</dev/tty; } 2>/dev/null; then
    fail "未检测到 Docker；非交互安装请设置 OSS_INSTALL_DOCKER=1"
  fi
  if ! read -r -p "未检测到 Docker，是否使用 Docker 官方脚本安装？[y/N] " answer <&3; then
    exec 3<&-
    fail "未检测到 Docker；非交互安装请设置 OSS_INSTALL_DOCKER=1"
  fi
  exec 3<&-
  [[ "$answer" =~ ^[Yy]([Ee][Ss])?$ ]]
}

install_docker() {
  command -v curl >/dev/null 2>&1 || fail "安装 Docker 需要 curl"
  local installer
  installer="$(mktemp)"
  info "下载 Docker 官方安装脚本"
  curl -fsSL https://get.docker.com -o "$installer"
  if ! run_privileged sh "$installer"; then
    rm -f "$installer"
    fail "Docker 安装失败"
  fi
  rm -f "$installer"
}

start_docker() {
  docker info >/dev/null 2>&1 && return 0
  if command -v systemctl >/dev/null 2>&1; then
    run_privileged systemctl enable --now docker || true
  elif command -v service >/dev/null 2>&1; then
    run_privileged service docker start || true
  fi
  docker info >/dev/null 2>&1 || fail "Docker 已安装但守护进程不可用，请启动 Docker 后重试"
}

restore_previous() {
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  if docker inspect "$CONTAINER-previous" >/dev/null 2>&1; then
    docker rename "$CONTAINER-previous" "$CONTAINER"
    docker start "$CONTAINER" >/dev/null
    info "新容器启动失败，已恢复原容器"
  fi
}

wait_healthy() {
  local status
  for ((attempt = 1; attempt <= 60; attempt++)); do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$CONTAINER" 2>/dev/null || true)"
    case "$status" in
      healthy) return 0 ;;
      exited|dead|unhealthy) return 1 ;;
    esac
    sleep 1
  done
  return 1
}

[[ "$(uname -s)" == "Linux" ]] || fail "一键安装脚本仅支持 Linux"
[[ "$PORT" =~ ^[0-9]+$ ]] && ((PORT >= 1 && PORT <= 65535)) || fail "OSS_PORT 必须是 1-65535"
[[ "$BIND_ADDRESS" == "127.0.0.1" || "$BIND_ADDRESS" == "0.0.0.0" ]] || fail "OSS_BIND_ADDRESS 仅支持 127.0.0.1 或 0.0.0.0"
[[ "$CONTAINER" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]+$ ]] || fail "OSS_CONTAINER_NAME 非法"
[[ "$VOLUME" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]+$ ]] || fail "OSS_DATA_VOLUME 非法"

if ! command -v docker >/dev/null 2>&1; then
  confirm_docker_install || fail "需要 Docker 才能继续安装"
  install_docker
fi
start_docker

info "拉取最新多架构镜像 $IMAGE"
if [[ "$SKIP_PULL" == "1" ]]; then
  docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "本地不存在镜像 $IMAGE"
else
  docker pull "$IMAGE"
fi
docker volume create "$VOLUME" >/dev/null

if docker inspect "$CONTAINER-previous" >/dev/null 2>&1; then
  docker rm -f "$CONTAINER-previous" >/dev/null
fi
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  info "停止现有容器，数据卷 $VOLUME 保持不变"
  docker stop --time 20 "$CONTAINER" >/dev/null || true
  docker rename "$CONTAINER" "$CONTAINER-previous"
fi

if ! docker run -d \
  --name "$CONTAINER" \
  --restart unless-stopped \
  --stop-timeout 20 \
  --label org.opencontainers.image.source=https://github.com/helantianshen/oss-sync \
  -p "${BIND_ADDRESS}:${PORT}:8080" \
  -v "${VOLUME}:/app/data" \
  --health-cmd 'wget -q -O /dev/null http://127.0.0.1:8080/readyz' \
  --health-interval 30s \
  --health-timeout 5s \
  --health-retries 3 \
  --health-start-period 10s \
  "$IMAGE" >/dev/null; then
  restore_previous
  fail "无法创建 OSS Sync 容器"
fi

info "等待服务健康检查"
if ! wait_healthy; then
  docker logs --tail 100 "$CONTAINER" >&2 || true
  restore_previous
  fail "服务未能健康启动"
fi

docker rm -f "$CONTAINER-previous" >/dev/null 2>&1 || true
info "OSS Sync 已启动，数据卷：$VOLUME"
if [[ "$BIND_ADDRESS" == "0.0.0.0" ]]; then
  info "访问地址：http://<服务器IP>:$PORT"
else
  info "访问地址：http://127.0.0.1:$PORT"
fi
info "首次注册成功的用户将成为管理员"
