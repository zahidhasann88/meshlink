# syntax=docker/dockerfile:1
FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o /meshlink ./cmd/meshlink && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o /meshlink-registry ./cmd/meshlink-registry
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 10001 meshlink && adduser -D -u 10001 -G meshlink meshlink
COPY --from=build /meshlink /meshlink-registry /usr/local/bin/
USER 10001:10001
WORKDIR /data
ENTRYPOINT ["meshlink"]
CMD ["--help"]
