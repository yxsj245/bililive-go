#!/usr/bin/env bash
# 正式部署管理入口；只依赖本目录配置与 Docker，不依赖开发目录。
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
cd "$base"
case "${1:-status}" in
  start)
    docker compose config -q
    docker compose up -d
    ;;
  stop)
    docker compose stop
    ;;
  restart)
    docker compose restart
    ;;
  status)
    docker compose ps
    ;;
  logs)
    docker compose logs --tail "${2:-100}" bililive-go
    ;;
  *)
    echo '用法：bash manage.sh {start|stop|restart|status|logs [行数]}' >&2
    exit 2
    ;;
esac
