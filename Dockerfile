# syntax=docker/dockerfile:1.7
# The build stage runs on the builder's platform and cross-compiles, so
# multi-arch images (linux/amd64 + linux/arm64) build fast without emulation.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS=linux TARGETARCH
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X github.com/abdulovia/cookcompass/internal/server.Version=${VERSION}" \
    -o /out/cookcompass ./cmd/cookcompass \
 && mkdir -p /out/data/backups /out/data/cache /out/data/catalog-inbox /out/data/recipe-inbox

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source=https://github.com/abdulovia/cookcompass \
      org.opencontainers.image.description="CookCompass: learn to cook with what your shops sell" \
      org.opencontainers.image.licenses=MIT
WORKDIR /app
COPY --from=build /out/cookcompass /app/cookcompass
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY seed/sources /app/seed/sources
ENV COOK_ADDR=:8080 \
    COOK_DB=/data/cookcompass.db \
    COOK_SCRAPE_CACHE=/data/cache/http \
    COOK_CATALOG_INBOX=/data/catalog-inbox \
    COOK_RECIPE_INBOX=/data/recipe-inbox \
    COOK_CONTENT_DIR=/data/content \
    COOK_SCRAPE_TARGETS=/app/seed/sources/jsonld_targets.txt \
    COOK_CATALOG_TARGETS=/app/seed/sources/catalog_targets.txt
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/app/cookcompass"]
CMD ["serve"]
