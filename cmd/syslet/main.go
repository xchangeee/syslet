package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"codeberg.org/xchangeee/syslet/internal/server/api"
	"codeberg.org/xchangeee/syslet/internal/server/daemon"
	"codeberg.org/xchangeee/syslet/internal/server/store"
	"codeberg.org/xchangeee/syslet/internal/server/systemd"
	pb "codeberg.org/xchangeee/syslet/proto"

	"github.com/spf13/afero"
	"google.golang.org/grpc"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create shared dependencies
	fs := afero.NewOsFs()

	// Create D-Bus connection
	dbusConn, err := systemd.NewDBusConnection(ctx)
	if err != nil {
		logger.Error("failed to connect to systemd", "error", err)
		os.Exit(1)
	}
	defer dbusConn.Close()

	// Create systemd client with injected dependencies
	sd := systemd.NewClient(dbusConn, fs)

	dbPath := store.DefaultDBPath
	if p := os.Getenv("SYSLET_DB"); p != "" {
		dbPath = p
	}

	st, err := store.New(fs, dbPath)
	if err != nil {
		logger.Error("failed to open store", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer st.Close()

	d := daemon.New(fs, logger, sd, st, daemon.Config{})

	// Start gRPC server
	listenAddr := ":7233"
	if addr := os.Getenv("SYSLET_LISTEN"); addr != "" {
		listenAddr = addr
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.Error("failed to listen", "addr", listenAddr, "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterSysletServiceServer(grpcServer, api.NewServer(d, logger))

	go func() {
		logger.Info("gRPC server listening", "addr", listenAddr)
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("gRPC server failed", "error", err)
		}
	}()

	// Run reconciliation loop
	if err := d.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
		os.Exit(1)
	}

	grpcServer.GracefulStop()
}
