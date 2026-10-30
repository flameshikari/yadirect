# syntax=docker/dockerfile:1
ARG BUN_VERSION=1.4.4
ARG GO_VERSION=1.27.3
FROM --platform=$BUILDPLATFORM dhi.io/bun:${BUN_VERSION}-dev AS frontend
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY client/ ./client/
RUN bun run build:frontend

FROM --platform=$BUILDPLATFORM dhi.io/golang:${GO_VERSION}-dev AS backend
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=1.0.0
WORKDIR /app
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go run ./tools/upx /usr/local/bin/upx
COPY --from=frontend /app/server/public ./public
RUN go mod verify && go test ./... && mkdir -p /data && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /yadirect . && \
    upx --best --lzma --no-progress /yadirect && upx -t /yadirect

FROM dhi.io/static:20230311 AS release
WORKDIR /app
ENV SQLITE_PATH=/app/data/yadirect.sqlite
COPY --from=backend --chown=65532:65532 /data ./data
COPY --from=backend /yadirect /usr/local/bin/yadirect
USER 65532:65532
VOLUME ["/app/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/yadirect"]
