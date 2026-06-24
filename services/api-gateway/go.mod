module github.com/opspilot/api-gateway

go 1.22

require (
	github.com/clerk/clerk-sdk-go/v2 v2.3.1
	github.com/joho/godotenv v1.5.1
	github.com/opspilot/gen v0.0.0
	github.com/rs/cors v1.11.0
	google.golang.org/grpc v1.64.0
)

require (
	github.com/go-jose/go-jose/v3 v3.0.3 // indirect
	golang.org/x/crypto v0.21.0 // indirect
	golang.org/x/net v0.22.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/opspilot/gen => ../../gen/go
