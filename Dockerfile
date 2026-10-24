# Build from the repository root. All defaults pin multi-platform image indexes.
ARG NODE_IMAGE=node:22.21.0-bookworm@sha256:23c24e85395992be118734a39903e08c8f7d1abc73978c46b6bda90060091a49
ARG GO_IMAGE=golang:1.27.2@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8
ARG RUNTIME_IMAGE=debian:13-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

FROM ${NODE_IMAGE} AS webui
WORKDIR /src
COPY package.json package-lock.json ./
COPY apps/webui/package.json apps/webui/package.json
COPY apps/website/package.json apps/website/package.json
COPY apps/ui-design/package.json apps/ui-design/package.json
COPY packages/sdk/package.json packages/sdk/package.json
COPY packages/ui/package.json packages/ui/package.json
COPY packages/ui-host/package.json packages/ui-host/package.json
RUN npm ci
COPY apps/webui apps/webui
COPY packages packages
COPY scripts scripts
COPY release/platforms.json release/platforms.json
COPY LICENSE THIRD_PARTY_NOTICES.md ./
ARG MOLR_VERSION=0.1.0-local
ARG MOLR_COMMIT=0000000000000000000000000000000000000000
RUN npm run build:webui

FROM ${GO_IMAGE} AS build
ENV GOTOOLCHAIN=local
WORKDIR /src/runtime
COPY runtime/go.mod runtime/go.sum runtime/.go-version ./
ARG GOPROXY=https://proxy.golang.org,direct
# Keep the pinned Go image aligned with both project version files.
RUN test "$(go env GOVERSION)" = "go$(cat .go-version)" && go mod download
COPY runtime ./
COPY --from=webui /src/apps/webui/dist ./internal/webassets/dist
ARG MOLR_VERSION=0.1.0-local
ARG MOLR_COMMIT=0000000000000000000000000000000000000000
ARG MOLR_CONTAINER_IMAGE=
ARG MOLR_BUILT_AT=
RUN CGO_ENABLED=0 go build -trimpath -tags webui \
    -ldflags "-s -w -X molr.ai/runtime/internal/buildinfo.Version=${MOLR_VERSION} -X molr.ai/runtime/internal/buildinfo.Commit=${MOLR_COMMIT} -X molr.ai/runtime/internal/buildinfo.BuiltAt=${MOLR_BUILT_AT} -X molr.ai/runtime/internal/buildinfo.ContainerImage=${MOLR_CONTAINER_IMAGE} -X molr.ai/runtime/internal/buildinfo.Distribution=container" \
    -o /out/molr ./cmd/molr && chmod 0755 /out/molr

FROM ${RUNTIME_IMAGE} AS runtime
ARG MOLR_VERSION=0.1.0-local
ARG MOLR_COMMIT=0000000000000000000000000000000000000000
ARG MOLR_SOURCE=https://github.com/molr-ai/molr
LABEL org.opencontainers.image.version=${MOLR_VERSION} \
      org.opencontainers.image.revision=${MOLR_COMMIT} \
      org.opencontainers.image.source=${MOLR_SOURCE} \
      org.opencontainers.image.licenses=MIT
RUN groupadd --gid 65532 molr && useradd --uid 65532 --gid molr --no-create-home --home-dir /data --shell /bin/sh molr \
    && mkdir -p /data && chown molr:molr /data && chmod 0700 /data
COPY --from=build --chmod=0755 /out/molr /usr/local/bin/molr
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/molr/
ENV HOME=/data
USER 65532:65532
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/molr"]
CMD ["serve", "-config", "/etc/molr/server.json"]

# Optional Linux sandbox image. The default target below remains the basic image.
FROM runtime AS isolated
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends systemd bubblewrap busybox-static util-linux ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY deploy/docker/molr-container.target deploy/docker/molr.service /etc/systemd/system/
COPY --chmod=0755 deploy/docker/isolated-entrypoint.sh /usr/local/bin/molr-container
ENV container=docker
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/usr/local/bin/molr-container"]
CMD ["serve", "-config", "/etc/molr/server.json"]

FROM build AS sandbox-tests
RUN CGO_ENABLED=0 go test -c -o /out/sandbox.test ./internal/platform/sandbox

FROM isolated AS isolation-test
RUN apt-get update && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=sandbox-tests --chmod=0755 /out/sandbox.test /usr/local/libexec/molr-sandbox.test
COPY deploy/docker/molr-isolation-test.service /etc/systemd/system/
COPY deploy/docker/molr-isolation-test.target /etc/systemd/system/molr-container.target

FROM runtime AS default
CMD ["container"]
