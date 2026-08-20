FROM golang:1.26.5 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY framework ./framework
COPY db/migrations ./db/migrations
COPY smc-server ./smc-server

RUN go test ./smc-server/...
RUN GOOS=linux CGO_ENABLED=0 go build -o /app/smc-server-bin ./smc-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /etc/aegiscore
COPY --from=builder /app/smc-server-bin ./smc-server

EXPOSE 15900
CMD ["./smc-server"]
