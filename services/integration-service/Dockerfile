# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY gen/go/go.mod gen/go/go.sum ./gen/go/
COPY services/integration-service/go.mod services/integration-service/go.sum ./services/integration-service/
WORKDIR /app/services/integration-service
RUN go mod download

WORKDIR /app
COPY gen/go ./gen/go
COPY services/integration-service ./services/integration-service
WORKDIR /app/services/integration-service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/integration-service .

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S -g 10001 opspilot \
    && adduser -S -D -H -u 10001 -G opspilot opspilot
WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/integration-service .
USER 10001:10001
EXPOSE 9004 8084
CMD ["./integration-service"]
