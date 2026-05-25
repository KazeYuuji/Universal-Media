FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM alpine:3.21
RUN apk add --no-cache ffmpeg python3 py3-pip py3-virtualenv && \
    python3 -m venv /venv && \
    /venv/bin/pip install yt-dlp && \
    ln -s /venv/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=builder /app/server /server
EXPOSE 8081
CMD ["/server"]
