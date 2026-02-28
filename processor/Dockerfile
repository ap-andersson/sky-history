# Build stage
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /processor .

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=build /processor /app/processor
COPY db/migrations /app/db/migrations

ENTRYPOINT ["/app/processor"]
