FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o oto-backend ./cmd/server/main.go

FROM alpine:latest

RUN apk add --no-cache python3 py3-pip ffmpeg ca-certificates \
    && pip install --no-cache-dir --break-system-packages yt-dlp \
    && adduser -D -u 10001 otouser \
    && mkdir -p /var/cache/oto/yt-dlp \
    && chown -R otouser:otouser /var/cache/oto/yt-dlp

WORKDIR /app

COPY --from=builder /app/oto-backend .

USER otouser

EXPOSE 8080

CMD ["./oto-backend"]