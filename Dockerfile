FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /subconfig ./cmd/subconfig

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 subconfig \
    && adduser -D -H -u 10001 -G subconfig subconfig \
    && mkdir -p /data \
    && chown subconfig:subconfig /data
COPY --from=build /subconfig /usr/local/bin/subconfig
USER 10001:10001
WORKDIR /data
EXPOSE 25500
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -T 3 -O /dev/null http://127.0.0.1:25500/healthz || exit 1
ENTRYPOINT ["subconfig"]
CMD ["serve", "--listen", ":25500"]
