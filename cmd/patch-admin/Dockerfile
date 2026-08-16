FROM golang:1.26.5 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY framework ./framework
COPY patching ./patching
COPY cmd/patch-admin ./cmd/patch-admin

RUN go test ./patching/... ./cmd/patch-admin
RUN GOOS=linux CGO_ENABLED=0 go build -o /app/patch-admin-bin ./cmd/patch-admin

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /etc/aegiscore
COPY --from=builder /app/patch-admin-bin ./patch-admin

ENTRYPOINT ["./patch-admin"]
