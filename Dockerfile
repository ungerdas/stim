# syntax=docker/dockerfile:1

# 1. Build the web UI. Vite writes it to internal/webui/dist.
FROM node:22-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Build the Go binary with the UI embedded.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/webui/dist/ internal/webui/dist/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/stim ./cmd/stim

# 3. Small runtime image.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 1000 stim \
    && mkdir /config && chown stim:stim /config
COPY --from=build /out/stim /usr/local/bin/stim
USER stim
ENV STIM_HOST=0.0.0.0 \
    STIM_PORT=7846 \
    STIM_CONFIG_DIR=/config
VOLUME /config
EXPOSE 7846
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:7846/api/health >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/stim"]
