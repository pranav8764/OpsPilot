# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY gen/go/go.mod gen/go/go.sum ./gen/go/
COPY services/workspace-service/go.mod services/workspace-service/go.sum ./services/workspace-service/
WORKDIR /app/services/workspace-service
RUN go mod download

WORKDIR /app
COPY gen/go ./gen/go
COPY services/workspace-service ./services/workspace-service
WORKDIR /app/services/workspace-service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/workspace-service .

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /app/workspace-service .
EXPOSE 9002 8082
CMD ["./workspace-service"]
