package logic

import (
	"context"
	"encoding/json"
	"errors"

	"cityflow/app/tracking/cmd/rpc/internal/svc"
	"cityflow/app/tracking/cmd/rpc/pb"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/rpckit"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func mapTrackingError(err error) error {
	if errors.Is(err, domain.ErrRateLimited) {
		return status.Error(codes.ResourceExhausted, "report location at most once every 10 seconds")
	}
	return rpckit.Error(err, "tracking")
}

func jsonReply(v any) (*pb.JsonReply, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, rpckit.Error(err, "tracking")
	}
	return &pb.JsonReply{BodyJson: data}, nil
}

type base struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func newBase(ctx context.Context, s *svc.ServiceContext) base {
	return base{ctx: ctx, svcCtx: s, Logger: logx.WithContext(ctx)}
}

type ReportLogic struct{ base }

func NewReportLogic(ctx context.Context, s *svc.ServiceContext) *ReportLogic {
	return &ReportLogic{newBase(ctx, s)}
}
func (l *ReportLogic) Report(in *pb.JsonBody) (*pb.JsonReply, error) {
	c, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := rpckit.RequireRole(auth.RoleRider, c.Role); err != nil {
		return nil, err
	}
	var input struct {
		location.Report
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := rpckit.DecodeBody(in.GetBodyJson(), &input); err != nil {
		return nil, mapTrackingError(err)
	}
	if input.Latitude == nil || input.Longitude == nil {
		return nil, status.Error(codes.InvalidArgument, "latitude and longitude are required")
	}
	input.Report.Point = location.Point{Latitude: *input.Latitude, Longitude: *input.Longitude}
	// task_id is only a realtime annotation. It is not used to resolve task
	// ownership or to write a per-task history row, so a stale task_id must not
	// reject a rider's live position report.
	out, err := l.svcCtx.Tracking.Report(l.ctx, c.Subject, input.Report)
	if err != nil {
		return nil, mapTrackingError(err)
	}
	return jsonReply(out)
}

type SnapshotLogic struct{ base }

func NewSnapshotLogic(ctx context.Context, s *svc.ServiceContext) *SnapshotLogic {
	return &SnapshotLogic{newBase(ctx, s)}
}
func (l *SnapshotLogic) Snapshot(in *pb.IdRequest) (*pb.JsonReply, error) {
	c, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Tracking.Snapshot(l.ctx, in.GetId(), *c)
	if err != nil {
		return nil, mapTrackingError(err)
	}
	return jsonReply(out)
}

type HistoryLogic struct{ base }

func NewHistoryLogic(ctx context.Context, s *svc.ServiceContext) *HistoryLogic {
	return &HistoryLogic{newBase(ctx, s)}
}
func (l *HistoryLogic) History(in *pb.IdBody) (*pb.JsonReply, error) {
	c, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var q struct {
		After uint64 `json:"after"`
		Limit int    `json:"limit"`
	}
	if len(in.GetBodyJson()) > 0 {
		if err := rpckit.DecodeBody(in.GetBodyJson(), &q); err != nil {
			return nil, mapTrackingError(err)
		}
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	out, err := l.svcCtx.Tracking.History(l.ctx, in.GetId(), *c, q.After, q.Limit)
	if err != nil {
		return nil, mapTrackingError(err)
	}
	return jsonReply(out)
}

type NearbyLogic struct{ base }

func NewNearbyLogic(ctx context.Context, s *svc.ServiceContext) *NearbyLogic {
	return &NearbyLogic{newBase(ctx, s)}
}
func (l *NearbyLogic) Nearby(in *pb.QueryRequest) (*pb.JsonReply, error) {
	c, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := rpckit.RequireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	var q struct {
		Latitude     float64 `json:"latitude"`
		Longitude    float64 `json:"longitude"`
		RadiusMeters float64 `json:"radius_meters"`
		Limit        int     `json:"limit"`
	}
	if err := rpckit.DecodeBody(in.GetQueryJson(), &q); err != nil {
		return nil, mapTrackingError(err)
	}
	if q.RadiusMeters == 0 {
		q.RadiusMeters = 5000
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	out, err := l.svcCtx.Tracking.Nearby(l.ctx, location.Point{Latitude: q.Latitude, Longitude: q.Longitude}, q.RadiusMeters, q.Limit)
	if err != nil {
		return nil, mapTrackingError(err)
	}
	return jsonReply(map[string]any{"data": out})
}

type GetPositionInternalLogic struct{ base }

func NewGetPositionInternalLogic(ctx context.Context, s *svc.ServiceContext) *GetPositionInternalLogic {
	return &GetPositionInternalLogic{newBase(ctx, s)}
}
func (l *GetPositionInternalLogic) GetPositionInternal(in *pb.IdRequest) (*pb.JsonReply, error) {
	if _, err := rpckit.Claims(l.ctx); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Positions.GetPosition(l.ctx, in.GetId())
	if err != nil {
		return nil, mapTrackingError(err)
	}
	return jsonReply(out)
}
