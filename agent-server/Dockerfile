FROM golang:1.26.5 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY framework ./framework
COPY db ./db
COPY gateway-server ./gateway-server
COPY agent-server ./agent-server
COPY protocol ./protocol

RUN go test ./...
RUN GOOS=linux CGO_ENABLED=0 go build -o /app/agent-server-bin ./agent-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /etc/aegiscore
COPY --from=builder /app/agent-server-bin ./agent-server

EXPOSE 15882
CMD ["./agent-server"]
