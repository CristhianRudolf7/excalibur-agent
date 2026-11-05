# Release archives are built natively with CGO and the web already embedded.
# Local: prepare bin/linux/<arch>/excalibur, then docker build .
FROM debian:bookworm-slim
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 excalibur \
    && useradd --uid 10001 --gid 10001 --home-dir /data excalibur \
    && mkdir -p /data && chown 10001:10001 /data
COPY --chmod=755 bin/linux/${TARGETARCH}/excalibur /usr/local/bin/excalibur
ENV HOME=/data XDG_DATA_HOME=/data/share XDG_CONFIG_HOME=/data/config XDG_CACHE_HOME=/data/cache XDG_RUNTIME_DIR=/data/run
USER 10001:10001
VOLUME /data
EXPOSE 7787
HEALTHCHECK --interval=30s --timeout=3s --start-period=30s CMD curl -fsS http://127.0.0.1:7787/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/excalibur"]
CMD ["daemon", "start", "--listen", "0.0.0.0:7787", "--allow-remote"]
