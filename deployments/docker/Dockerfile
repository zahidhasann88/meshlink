# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN for cmd in meshlink meshlink-registry meshlink-relay; do \
      CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid= -X github.com/zahidhasann88/meshlink/internal/app.Version=${VERSION}" -o /out/$cmd ./cmd/$cmd || exit 1; \
    done
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 10001 meshlink && adduser -D -u 10001 -G meshlink meshlink
COPY --from=build /out/ /usr/local/bin/
USER 10001:10001
WORKDIR /data
ENTRYPOINT ["meshlink"]
CMD ["--help"]
