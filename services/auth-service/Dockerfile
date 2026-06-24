# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates

COPY gen/go/go.mod gen/go/go.sum ./gen/go/
COPY services/auth-service/go.mod services/auth-service/go.sum ./services/auth-service/
WORKDIR /app/services/auth-service
RUN go mod download

WORKDIR /app
COPY gen/go ./gen/go
COPY services/auth-service ./services/auth-service
WORKDIR /app/services/auth-service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/auth-service .

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates wget

WORKDIR /app
COPY --from=builder /app/auth-service .

EXPOSE 9001 8081

CMD ["./auth-service"]
