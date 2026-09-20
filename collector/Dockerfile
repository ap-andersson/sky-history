# Build stage
#
# NOTE: the build context is the REPOSITORY ROOT, not this directory, because
# this service depends on the sibling shared/ module through a relative replace
# directive. Build with:
#
#   docker build -f collector/Dockerfile .
#
# See shared/README.md.
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git

WORKDIR /src

# The shared module first: it is small and changes rarely, so it caches well.
COPY shared/ ./shared/

# Cache dependencies. The replace directive resolves to /src/shared, which is
# the same relative path as on disk.
COPY collector/go.mod collector/go.sum ./collector/
WORKDIR /src/collector
RUN go mod download

# Build
COPY collector/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /collector .

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=build /collector /app/collector

ENTRYPOINT ["/app/collector"]
