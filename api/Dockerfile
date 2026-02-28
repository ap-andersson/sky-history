# Build stage
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /api .

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=build /api /app/api

ENTRYPOINT ["/app/api"]
