# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY gen/go/go.mod gen/go/go.sum ./gen/go/
COPY services/project-service/go.mod services/project-service/go.sum ./services/project-service/
WORKDIR /app/services/project-service
RUN go mod download

WORKDIR /app
COPY gen/go ./gen/go
COPY services/project-service ./services/project-service
WORKDIR /app/services/project-service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/project-service .

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S -g 10001 opspilot \
    && adduser -S -D -H -u 10001 -G opspilot opspilot
WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/project-service .
USER 10001:10001
EXPOSE 9003 8083
CMD ["./project-service"]
