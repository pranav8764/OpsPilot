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
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /app/integration-service .
EXPOSE 9004 8084
CMD ["./integration-service"]
