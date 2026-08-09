FROM golang:1.26.5 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY framework ./framework
COPY db/migrations ./db/migrations
COPY gateway-server ./gateway-server
COPY protocol ./protocol

RUN go test ./...
RUN GOOS=linux CGO_ENABLED=0 go build -o /app/gateway-server-bin ./gateway-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /etc/aegiscore
COPY --from=builder /app/gateway-server-bin ./gateway-server

EXPOSE 15779
CMD ["./gateway-server"]
