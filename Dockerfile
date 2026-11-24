FROM golang:1.27-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

COPY . .

ARG VERSION=dev

RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X telegram-group-mention-bot/internal/buildinfo.Version=${VERSION}" -o app ./cmd/bot

FROM alpine:latest

WORKDIR /app

COPY --from=builder /build/app .

VOLUME /data

ENV TELEGRAM_BOT_TOKEN="" \
    DATABASE_PATH="/data/data.sqlite"

CMD ["/app/app"]
