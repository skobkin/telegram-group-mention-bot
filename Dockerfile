FROM golang:1.27-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev

RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X telegram-group-mention-bot/internal/buildinfo.Version=${VERSION}" \
    -o /out/app ./cmd/bot

FROM alpine:3.22

RUN addgroup -S -g 10001 bot && adduser -S -u 10001 -G bot bot \
    && mkdir /data && chown bot:bot /data

WORKDIR /app

COPY --from=builder /out/app .

USER bot

VOLUME /data

ENV TELEGRAM_BOT_TOKEN="" \
    DATABASE_PATH="/data/data.sqlite"

CMD ["/app/app"]
