# 构建前先按项目要求运行 make build-web dev，再运行 make build 生成内嵌网页的发布版。
ARG MEDIA_IMAGE=bililive-rk3588-media:20261009
FROM ${MEDIA_IMAGE}
ARG SOURCE_REVISION=unknown
LABEL org.opencontainers.image.title="bililive-go RK3588" org.opencontainers.image.revision="${SOURCE_REVISION}"
ENV IS_DOCKER=true HOME=/srv/bililive BILILIVE_DISABLE_SELF_UPDATE=1
# 保留原有 .NET 录播姬/FLV 修复工具所需运行库。
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libicu74 libgssapi-krb5-2 && apt-get clean
COPY bin/bililive-linux-arm64 /usr/local/bin/bililive-go
COPY contrib/rk3588/entrypoint.sh /usr/local/bin/rk3588-entrypoint
RUN chmod 755 /usr/local/bin/bililive-go /usr/local/bin/rk3588-entrypoint && mkdir -p /etc/bililive-go /srv/bililive /opt/bililive/tools
WORKDIR /srv/bililive
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=90s --retries=3 CMD curl -fsS http://127.0.0.1:8080/api/info >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/rk3588-entrypoint"]
