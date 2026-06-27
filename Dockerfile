# syntax=docker/dockerfile:1.7
FROM golang:1.24-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w -X github.com/abdulovia/cookcompass/internal/server.Version=${VERSION}" \
    -o /out/cookcompass ./cmd/cookcompass \
 && mkdir -p /out/data/backups /out/data/cache /out/data/catalog-inbox /out/data/recipe-inbox

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/cookcompass /app/cookcompass
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY seed/sources /app/seed/sources
ENV COOK_ADDR=:8080 \
    COOK_DB=/data/cookcompass.db \
    COOK_SCRAPE_CACHE=/data/cache/http \
    COOK_CATALOG_INBOX=/data/catalog-inbox \
    COOK_RECIPE_INBOX=/data/recipe-inbox \
    COOK_SCRAPE_TARGETS=/app/seed/sources/jsonld_targets.txt \
    COOK_CATALOG_TARGETS=/app/seed/sources/catalog_targets.txt
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/app/cookcompass"]
CMD ["serve"]
