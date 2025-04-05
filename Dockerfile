FROM golang:1-alpine as builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

COPY . .

RUN CGO_ENABLED=1 go build -o app

FROM alpine:latest

WORKDIR /app

COPY --from=builder /build/app .

VOLUME /data

ENV TELEGRAM_BOT_TOKEN="" \
    DATABASE_PATH="/data/data.sqlite"

CMD ["/app/app"]
