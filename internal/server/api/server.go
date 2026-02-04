// Package api implements the gRPC server for syslet.
package api

import (
	"context"
	"log/slog"

	pb "codeberg.org/xchangeee/syslet/proto"

	"codeberg.org/xchangeee/syslet/internal/server/daemon"
	"codeberg.org/xchangeee/syslet/internal/server/journal"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the SysletService gRPC service.
type Server struct {
	pb.UnimplementedSysletServiceServer

	daemon *daemon.Daemon
	logger *slog.Logger
}

// NewServer creates a new gRPC server.
func NewServer(d *daemon.Daemon, logger *slog.Logger) *Server {
	return &Server{
		daemon: d,
		logger: logger,
	}
}

// ---------------------------------------------------------------------------
// Container RPCs
// ---------------------------------------------------------------------------

func (s *Server) ApplyContainers(ctx context.Context, req *pb.ApplyContainersRequest) (*pb.ApplyContainersResponse, error) {
	if len(req.Specs) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one spec is required")
	}
	results, err := s.daemon.ApplyContainers(ctx, req.Specs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "apply containers: %v", err)
	}
	return &pb.ApplyContainersResponse{Results: results}, nil
}

func (s *Server) GetContainer(ctx context.Context, req *pb.GetContainerRequest) (*pb.GetContainerResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	cs, err := s.daemon.GetContainer(ctx, req.Name)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "container %s: %v", req.Name, err)
	}
	return &pb.GetContainerResponse{Container: cs}, nil
}

func (s *Server) ListContainers(ctx context.Context, req *pb.ListContainersRequest) (*pb.ListContainersResponse, error) {
	containers, err := s.daemon.ListContainers(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing containers: %v", err)
	}
	return &pb.ListContainersResponse{Containers: containers}, nil
}

func (s *Server) DeleteContainer(ctx context.Context, req *pb.DeleteContainerRequest) (*pb.DeleteContainerResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := s.daemon.DeleteContainer(ctx, req.Name); err != nil {
		return nil, status.Errorf(codes.Internal, "delete container: %v", err)
	}
	return &pb.DeleteContainerResponse{Message: "deleted " + req.Name}, nil
}

// ---------------------------------------------------------------------------
// Volume RPCs
// ---------------------------------------------------------------------------

func (s *Server) ApplyVolumes(ctx context.Context, req *pb.ApplyVolumesRequest) (*pb.ApplyVolumesResponse, error) {
	if len(req.Specs) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one spec is required")
	}
	results, err := s.daemon.ApplyVolumes(ctx, req.Specs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "apply volumes: %v", err)
	}
	return &pb.ApplyVolumesResponse{Results: results}, nil
}

func (s *Server) GetVolume(ctx context.Context, req *pb.GetVolumeRequest) (*pb.GetVolumeResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	vs, err := s.daemon.GetVolume(ctx, req.Name)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "volume %s: %v", req.Name, err)
	}
	return &pb.GetVolumeResponse{Volume: vs}, nil
}

func (s *Server) ListVolumes(ctx context.Context, req *pb.ListVolumesRequest) (*pb.ListVolumesResponse, error) {
	volumes, err := s.daemon.ListVolumes(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing volumes: %v", err)
	}
	return &pb.ListVolumesResponse{Volumes: volumes}, nil
}

func (s *Server) DeleteVolume(ctx context.Context, req *pb.DeleteVolumeRequest) (*pb.DeleteVolumeResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := s.daemon.DeleteVolume(ctx, req.Name); err != nil {
		return nil, status.Errorf(codes.Internal, "delete volume: %v", err)
	}
	return &pb.DeleteVolumeResponse{Message: "deleted " + req.Name}, nil
}

// ---------------------------------------------------------------------------
// Network RPCs
// ---------------------------------------------------------------------------

func (s *Server) ApplyNetworks(ctx context.Context, req *pb.ApplyNetworksRequest) (*pb.ApplyNetworksResponse, error) {
	if len(req.Specs) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one spec is required")
	}
	results, err := s.daemon.ApplyNetworks(ctx, req.Specs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "apply networks: %v", err)
	}
	return &pb.ApplyNetworksResponse{Results: results}, nil
}

func (s *Server) GetNetwork(ctx context.Context, req *pb.GetNetworkRequest) (*pb.GetNetworkResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	ns, err := s.daemon.GetNetwork(ctx, req.Name)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "network %s: %v", req.Name, err)
	}
	return &pb.GetNetworkResponse{Network: ns}, nil
}

func (s *Server) ListNetworks(ctx context.Context, req *pb.ListNetworksRequest) (*pb.ListNetworksResponse, error) {
	networks, err := s.daemon.ListNetworks(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing networks: %v", err)
	}
	return &pb.ListNetworksResponse{Networks: networks}, nil
}

func (s *Server) DeleteNetwork(ctx context.Context, req *pb.DeleteNetworkRequest) (*pb.DeleteNetworkResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if err := s.daemon.DeleteNetwork(ctx, req.Name); err != nil {
		return nil, status.Errorf(codes.Internal, "delete network: %v", err)
	}
	return &pb.DeleteNetworkResponse{Message: "deleted " + req.Name}, nil
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

func (s *Server) Logs(req *pb.LogsRequest, stream pb.SysletService_LogsServer) error {
	if req.UnitName == "" {
		return status.Error(codes.InvalidArgument, "unit_name is required")
	}

	ctx := stream.Context()

	if !req.Follow {
		entries, err := journal.Read(ctx, req.UnitName, int(req.Lines))
		if err != nil {
			return status.Errorf(codes.Internal, "reading logs: %v", err)
		}
		for _, e := range entries {
			if err := stream.Send(&pb.LogEntry{
				Timestamp: e.Timestamp,
				Message:   e.Message,
				Priority:  e.Priority,
			}); err != nil {
				return err
			}
		}
		return nil
	}

	ch := make(chan journal.Entry, 100)
	go func() {
		_ = journal.Follow(ctx, req.UnitName, int(req.Lines), ch)
	}()

	for entry := range ch {
		if err := stream.Send(&pb.LogEntry{
			Timestamp: entry.Timestamp,
			Message:   entry.Message,
			Priority:  entry.Priority,
		}); err != nil {
			return err
		}
	}

	return nil
}
