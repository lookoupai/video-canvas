# syntax=docker/dockerfile:1
# 构建上下文是仓库根目录：docker build -f backend/Dockerfile .
# 画布 Agent 是独立的服务（agent/Dockerfile，镜像 video-canvas-agent），这个镜像里没有 Node。

FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILDTIME=unknown
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOMAXPROCS=2 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -p 1 -trimpath -ldflags "-s -w -X video-canvas/pkg/version.Version=${VERSION} -X video-canvas/pkg/version.GitCommit=${COMMIT} -X video-canvas/pkg/version.BuildTime=${BUILDTIME}" \
    -o /out/server ./cmd/server

# Python 原生依赖提供 glibc 的 amd64/arm64 wheel，使用 Debian 保持两种架构一致。
# 独立 target 可在 CI 中验证实际分析，无需先编译 Go。
FROM python:3.12-slim-bookworm AS reference-runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates ffmpeg libgomp1 wget && rm -rf /var/lib/apt/lists/*
COPY backend/reference-runtime/ /opt/reference-build/
RUN sh /opt/reference-build/install.sh && rm -rf /opt/reference-build
ENV PATH="/opt/rea-video/bin:${PATH}" \
    REA_VIDEO_MODELS=/opt/rea-video/models \
    PYTHONDONTWRITEBYTECODE=1 \
    OMP_NUM_THREADS=1 \
    OPENBLAS_NUM_THREADS=1 \
    MKL_NUM_THREADS=1 \
    OPENCV_FOR_THREADS_NUM=1
# 保持原 Alpine 镜像的 UID/GID，已有素材、日志和 socket 卷无需修改属主。
RUN groupadd --gid 101 app && useradd --uid 100 --gid 101 --create-home --home-dir /home/app app
USER app:app

FROM reference-runtime AS runtime
WORKDIR /app
USER root
COPY --from=build /out/server /app/server
COPY --from=build /src/configs/config.yaml /app/configs/config.yaml
# /run/plugin-runner 是 backend 与 plugin-runner 容器共享 Unix socket 的卷挂载点，首次挂载时命名卷继承这里的属主
# /app/logs 预先建好并交给 app 用户：命名卷首次挂载时继承这个属主，否则日志写不进去
RUN mkdir -p /app/data/assets /app/logs /run/plugin-runner && chown -R app:app /app /run/plugin-runner

USER app:app
EXPOSE 8080
ENTRYPOINT ["/app/server", "-c", "/app/configs/config.yaml"]
