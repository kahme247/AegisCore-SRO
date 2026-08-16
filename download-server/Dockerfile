FROM golang:1.26.5 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY framework ./framework
COPY download-server ./download-server

RUN go test ./download-server/... ./framework/server ./framework/network/...
RUN GOOS=linux CGO_ENABLED=0 go build -o /app/download-server-bin ./download-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /etc/aegiscore
COPY --from=builder /app/download-server-bin ./download-server

EXPOSE 15880
EXPOSE 15881
CMD ["./download-server"]
