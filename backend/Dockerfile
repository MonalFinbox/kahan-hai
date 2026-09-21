# Build
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# modernc.org/sqlite is pure Go, so CGO stays off and the binary is static.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

# Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app

COPY --from=build /out/server /usr/local/bin/server

# Owned by app so SQLite can write when the volume mounts here.
RUN mkdir -p /data && chown app:app /data
USER app
VOLUME /data

ENV DB_PATH=/data/kahan-hai.db \
    ADDR=:8080
EXPOSE 8080

ENTRYPOINT ["server"]
