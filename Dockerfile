# syntax=docker/dockerfile:1

# --- Stage 1: build the React frontend ---
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
  npm ci
COPY web/ ./
RUN npm run build

# --- Stage 2: build the Go binary with the frontend embedded ---
FROM golang:1.26-alpine AS build
WORKDIR /src
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download
COPY . .
# Overwrite the committed placeholder with the freshly built frontend.
COPY --from=web /src/web/dist ./internal/webui/dist
# Source maps are for debugging the build, not for shipping in the binary.
RUN find ./internal/webui/dist -name '*.map' -type f -delete
# Cache mounts persist Go's module and build caches across `docker build`
# invocations (they otherwise live outside any image layer and are lost the
# moment a source file changes and invalidates this RUN), so incremental
# builds only recompile what actually changed instead of every dependency.
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 go build -trimpath -ldflags \
  "-s -w \
  -X 'github.com/Jishnu-Prasad888/popcorn/internal/version.Version=${VERSION}' \
  -X 'github.com/Jishnu-Prasad888/popcorn/internal/version.Commit=${COMMIT}' \
  -X 'github.com/Jishnu-Prasad888/popcorn/internal/version.BuildDate=${BUILD_DATE}'" \
  -o /out/popcorn ./cmd/popcorn

# --- Stage 3: minimal runtime ---
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata ffmpeg
RUN addgroup -S popcorn && adduser -S popcorn -G popcorn \
  && mkdir -p /data && chown popcorn:popcorn /data
COPY --from=build /out/popcorn /usr/local/bin/popcorn
USER popcorn
ENV POPCORN_DATA_DIR=/data
ENV POPCORN_HTTP_ADDR=:8715
VOLUME /data
EXPOSE 8715
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8715/api/v1/ready || exit 1
ENTRYPOINT ["/usr/local/bin/popcorn"]