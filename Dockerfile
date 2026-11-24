# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend-builder
WORKDIR /build
COPY code-common/frontend /build/code-common/frontend
COPY code-common/build-utils /build/code-common/build-utils
COPY code-markdocx/frontend/package.json /build/code-markdocx/frontend/package.json
RUN cd /build/code-markdocx/frontend && npm install --no-package-lock
COPY code-markdocx/frontend /build/code-markdocx/frontend
RUN cd /build/code-markdocx/frontend && npm run build

FROM golang:1.26-alpine AS backend-builder
WORKDIR /build
COPY code-common/backend /build/code-common/backend
COPY code-markdocx /build/code-markdocx
COPY --from=frontend-builder /build/code-markdocx/frontend/dist /build/code-markdocx/frontend/dist
RUN cd /build/code-markdocx && CGO_ENABLED=0 go build -o /out/code-markdocx-server .

# Slim image (pure Go binary, zero external runtime dependencies)
FROM debian:bookworm-slim AS slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend-builder /out/code-markdocx-server /app/code-markdocx-server
COPY code-markdocx/config.yaml.example /app/config.yaml
EXPOSE 8105
ENTRYPOINT ["/app/code-markdocx-server"]
CMD ["-config", "/app/config.yaml"]

# Full image (adds headless LibreOffice Draw + CJK fonts for .emf/.wmf vector diagrams)
FROM slim AS full
RUN apt-get update && apt-get install -y --no-install-recommends \
    libreoffice-draw \
    fonts-noto-cjk \
  && rm -rf /var/lib/apt/lists/*
