FROM golang:1.26.5 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY framework ./framework
COPY db/migrations ./db/migrations
COPY cmd/migrate ./cmd/migrate

RUN go test ./cmd/migrate ./db/migrations ./framework/db
RUN GOOS=linux CGO_ENABLED=0 go build -trimpath -o /app/migrate ./cmd/migrate

FROM alpine:3.22
RUN apk --no-cache add ca-certificates
WORKDIR /etc/aegiscore
COPY --from=builder /app/migrate ./migrate

# The job needs no writable application directory or root privileges.
USER 65532:65532
ENTRYPOINT ["./migrate"]
