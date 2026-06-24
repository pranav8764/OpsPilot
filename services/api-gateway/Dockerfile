# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY gen/go/go.mod gen/go/go.sum ./gen/go/
COPY services/api-gateway/go.mod services/api-gateway/go.sum ./services/api-gateway/
WORKDIR /app/services/api-gateway
RUN go mod download

WORKDIR /app
COPY gen/go ./gen/go
COPY services/api-gateway ./services/api-gateway
WORKDIR /app/services/api-gateway
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/api-gateway .

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /app/api-gateway .
EXPOSE 8080
CMD ["./api-gateway"]
