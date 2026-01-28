// Package api implements the gRPC server for syslet.
package api

import (
	"context"
	"log/slog"
	"time"

	pb "codeberg.org/xchangeee/syslet/proto"

	"codeberg.org/xchangeee/syslet/internal/server/daemon"
	"codeberg.org/xchangeee/syslet/internal/server/journal"
	"codeberg.org/xchangeee/syslet/internal/server/parser"

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
		resp.Units = append(resp.Units, unitStatusToPb(us))
		return resp, nil
	}

	// All units
	units, err := s.daemon.ListUnits(ctx, parser.UnitTypeUnknown)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing units: %v", err)
	}
	for _, us := range units {
		resp.Units = append(resp.Units, unitStatusToPb(&us))
	}

	return resp, nil
}

func (s *Server) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	typeFilter := parserUnitTypeFromPb(req.TypeFilter)

	units, err := s.daemon.ListUnits(ctx, typeFilter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing units: %v", err)
	}

	resp := &pb.ListResponse{}
	for _, us := range units {
		resp.Units = append(resp.Units, unitStatusToPb(&us))
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

// unitStatusToPb converts a domain UnitStatus to protobuf.
func unitStatusToPb(us *daemon.UnitStatus) *pb.UnitStatus {
	pbus := &pb.UnitStatus{
		Name:         us.Name,
		Type:         pbUnitType(us.Type),
		DesiredState: pbDesiredState(us.DesiredState),
		ActiveState:  pbActiveState(us.ActiveState),
		Enabled:      us.Enabled,
		ConfigFiles:  us.ConfigFiles,
		Error:        us.Error,
	}
	if !us.LastReconciled.IsZero() {
		pbus.LastReconciled = us.LastReconciled.Format(time.RFC3339)
	}
	return pbus
}

// parserUnitTypeFromPb converts a protobuf UnitType to parser.UnitType.
func parserUnitTypeFromPb(t pb.UnitType) parser.UnitType {
	switch t {
	case pb.UnitType_UNIT_TYPE_CONTAINER:
		return parser.UnitTypeContainer
	case pb.UnitType_UNIT_TYPE_VOLUME:
		return parser.UnitTypeVolume
	case pb.UnitType_UNIT_TYPE_NETWORK:
		return parser.UnitTypeNetwork
	default:
		return parser.UnitTypeUnknown
	}
}

func pbUnitType(t parser.UnitType) pb.UnitType {
	switch t {
	case parser.UnitTypeContainer:
		return pb.UnitType_UNIT_TYPE_CONTAINER
	case parser.UnitTypeVolume:
		return pb.UnitType_UNIT_TYPE_VOLUME
	case parser.UnitTypeNetwork:
		return pb.UnitType_UNIT_TYPE_NETWORK
	default:
		return pb.UnitType_UNIT_TYPE_UNSPECIFIED
	}
}

func pbActiveState(s string) pb.ActiveState {
	switch s {
	case "active":
		return pb.ActiveState_ACTIVE_STATE_ACTIVE
	case "inactive":
		return pb.ActiveState_ACTIVE_STATE_INACTIVE
	case "failed":
		return pb.ActiveState_ACTIVE_STATE_FAILED
	case "activating":
		return pb.ActiveState_ACTIVE_STATE_ACTIVATING
	case "deactivating":
		return pb.ActiveState_ACTIVE_STATE_DEACTIVATING
	default:
		return pb.ActiveState_ACTIVE_STATE_UNSPECIFIED
	}
}

func pbDesiredState(s string) pb.DesiredState {
	switch s {
	case "running":
		return pb.DesiredState_DESIRED_STATE_RUNNING
	case "stopped":
		return pb.DesiredState_DESIRED_STATE_STOPPED
	default:
		return pb.DesiredState_DESIRED_STATE_UNSPECIFIED
	}
}
