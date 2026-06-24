package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	workspacev1 "github.com/opspilot/gen/proto/workspace/v1"
	"github.com/opspilot/workspace-service/internal/config"
	"github.com/opspilot/workspace-service/internal/db"
	"github.com/opspilot/workspace-service/internal/server"
)

func main() {
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db connect failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("database connected ✅")

	go startHealthServer(cfg.HealthPort, pool, logger)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.GRPCPort))
	if err != nil {
		logger.Error("listen failed", "error", err)
		os.Exit(1)
	}

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingInterceptor(logger),
			recoveryInterceptor(logger),
		),
	)

	workspacev1.RegisterWorkspaceServiceServer(grpcSrv, server.New(pool, logger))
	reflection.Register(grpcSrv)

	logger.Info("workspace-service started ✅",
		"grpc_port", cfg.GRPCPort,
		"health_port", cfg.HealthPort,
	)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		logger.Info("shutting down workspace-service...")
		grpcSrv.GracefulStop()
	}()

	if err := grpcSrv.Serve(lis); err != nil {
		logger.Error("grpc server failed", "error", err)
		os.Exit(1)
	}
}

func startHealthServer(port string, pool *pgxpool.Pool, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		status := "ok"
		if err := pool.Ping(ctx); err != nil {
			status = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":%q,"service":"workspace-service"}`, status)
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%s", port), Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	logger.Info("health server listening", "port", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("health server error", "error", err)
	}
}

func loggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logger.Info("grpc", "method", info.FullMethod, "duration_ms", time.Since(start).Milliseconds(), "error", err)
		return resp, err
	}
}

func recoveryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic", "method", info.FullMethod, "panic", r)
				err = fmt.Errorf("internal error")
			}
		}()
		return handler(ctx, req)
	}
}
