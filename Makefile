.PHONY: up down migrate health logs clean build proto

# ── Docker Compose ──────────────────────────────────────────────────────────

up:
	docker compose up --build -d
	@echo "✅ All services started. Frontend: http://localhost:3000 | Gateway: http://localhost:8080"

down:
	docker compose down

clean:
	docker compose down -v --remove-orphans
	@echo "🗑  Volumes and containers removed"

logs:
	docker compose logs -f

# ── Database ──────────────────────────────────────────────────────────────────

migrate:
	@echo "⏳ Running migrations..."
	@for migration in infra/migrations/*.sql; do \
		echo "→ Applying $$migration"; \
		docker compose exec -T postgres psql -U opspilot -d opspilot -f /docker-entrypoint-initdb.d/$$(basename $$migration) || \
		PGPASSWORD=opspilot_secret psql -h localhost -U opspilot -d opspilot -f $$migration || exit 1; \
	done
	@echo "✅ Migrations complete"

# ── Health Checks ─────────────────────────────────────────────────────────────

health:
	@echo "🔍 Checking service health..."
	@curl -sf http://localhost:8080/health && echo "✅ API Gateway" || echo "❌ API Gateway"
	@curl -sf http://localhost:8081/health && echo "✅ Auth Service" || echo "❌ Auth Service"
	@curl -sf http://localhost:8082/health && echo "✅ Workspace Service" || echo "❌ Workspace Service"
	@curl -sf http://localhost:8083/health && echo "✅ Project Service" || echo "❌ Project Service"
	@curl -sf http://localhost:8084/health && echo "✅ Integration Service" || echo "❌ Integration Service"

# ── Proto Generation (requires buf CLI: brew install bufbuild/buf/buf) ─────────

proto:
	@echo "⚙️  Generating proto code..."
	PATH="$$HOME/go/bin:$$PATH" buf generate
	@echo "✅ Proto code generated in gen/"

# ── Individual Service Dev ────────────────────────────────────────────────────

run-gateway:
	cd services/api-gateway && go run ./...

run-auth:
	cd services/auth-service && go run ./...

run-workspace:
	cd services/workspace-service && go run ./...

run-project:
	cd services/project-service && go run ./...

run-integration:
	cd services/integration-service && go run ./...

run-frontend:
	cd frontend && npm run dev

# ── Build all Go services ────────────────────────────────────────────────────

build:
	@echo "🔨 Building all Go services..."
	cd services/api-gateway && go build -o bin/gateway .
	cd services/auth-service && go build -o bin/auth .
	cd services/workspace-service && go build -o bin/workspace .
	cd services/project-service && go build -o bin/project .
	cd services/integration-service && go build -o bin/integration .
	@echo "✅ All services built"

# ── Test ──────────────────────────────────────────────────────────────────────

test:
	cd services/api-gateway && go test ./...
	cd services/auth-service && go test ./...
	cd services/workspace-service && go test ./...
	cd services/project-service && go test ./...
	cd services/integration-service && go test ./...
