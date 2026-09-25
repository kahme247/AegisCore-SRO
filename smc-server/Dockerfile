FROM golang:1.26.5 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY framework ./framework
COPY db/migrations ./db/migrations
COPY agent-server ./agent-server
COPY cmd/sro-data ./cmd/sro-data
COPY patching ./patching
COPY protocol ./protocol
COPY smc-server ./smc-server

RUN go test ./smc-server/...
RUN GOOS=linux CGO_ENABLED=0 go build -o /app/smc-server-bin ./smc-server
RUN GOOS=linux CGO_ENABLED=0 go build -o /app/sro-data-bin ./cmd/sro-data

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /etc/aegiscore
COPY --from=builder /app/smc-server-bin ./smc-server
COPY --from=builder /app/sro-data-bin ./sro-data

EXPOSE 15900
CMD ["./smc-server"]
