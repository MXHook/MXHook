FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o mxhook .

FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40
RUN apk add --no-cache ca-certificates \
    && addgroup -g 1000 mxhook \
    && adduser -D -H -u 1000 -G mxhook mxhook \
    && mkdir -p /data \
    && chown mxhook:mxhook /data \
    && chmod 0700 /data
WORKDIR /app
COPY --from=builder /app/mxhook .
COPY --from=builder /app/LICENSE /app/NOTICE ./
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
USER mxhook:mxhook
ENV MXHOOK_SMTP_ADDR=:2525 \
    MXHOOK_DATABASE_URL=/data/mxhook.db
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/readyz || exit 1
ENTRYPOINT ["./mxhook"]
CMD ["start"]
