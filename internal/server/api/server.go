// Package api implements the gRPC server for syslet.
package api

import (
	"context"
	"log/slog"
	"time"

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

func (s *Server) Apply(ctx context.Context, req *pb.ApplyRequest) (*pb.ApplyResponse, error) {
	if err := s.daemon.ApplySpecs(ctx, req.Specs); err != nil {
		return nil, status.Errorf(codes.Internal, "apply specs: %v", err)
	}

	return &pb.ApplyResponse{}, nil
}

func (s *Server) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	resp := &pb.StatusResponse{}

	if req.UnitName != "" {
		us, err := s.daemon.GetStatus(ctx, req.UnitName)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "unit %s: %v", req.UnitName, err)
		}
		resp.Units = append(resp.Units, managedUnitStateToPb(us))
		return resp, nil
	}

	// All units
	units, err := s.daemon.ListUnits(ctx, pb.UnitType_UNIT_TYPE_UNSPECIFIED)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing units: %v", err)
	}
	for _, us := range units {
		resp.Units = append(resp.Units, managedUnitStateToPb(&us))
	}

	return resp, nil
}

func (s *Server) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	units, err := s.daemon.ListUnits(ctx, req.TypeFilter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing units: %v", err)
	}

	resp := &pb.ListResponse{}
	for _, us := range units {
		resp.Units = append(resp.Units, managedUnitStateToPb(&us))
	}

	return resp, nil
}

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

func (s *Server) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if req.UnitName == "" {
		return nil, status.Error(codes.InvalidArgument, "unit_name is required")
	}

	if err := s.daemon.DeleteUnit(ctx, req.UnitName); err != nil {
		// Check if it's a not found error
		if err.Error() == "unit \""+req.UnitName+"\" not found" {
			return nil, status.Errorf(codes.NotFound, "%v", err)
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}

	return &pb.DeleteResponse{
		Message: "deleted " + req.UnitName,
	}, nil
}

// managedUnitStateToPb converts a domain ManagedUnitState to protobuf UnitStatus.
func managedUnitStateToPb(us *daemon.ManagedUnitState) *pb.UnitStatus {
	pbus := &pb.UnitStatus{
		Name:         us.Name,
		Type:         us.Type,
		DesiredState: us.DesiredState,
		ActiveState:  us.ActiveState,
		Enabled:      us.Enabled,
		ConfigFiles:  us.ConfigFiles,
		Error:        us.Error,
	}
	if !us.LastReconciled.IsZero() {
		pbus.LastReconciled = us.LastReconciled.Format(time.RFC3339)
	}
	return pbus
}
