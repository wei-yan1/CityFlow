package logic

import (
	"context"
	"encoding/json"

	"cityflow/app/task/cmd/rpc/internal/svc"
	"cityflow/app/task/cmd/rpc/pb"
	"cityflow/app/task/domain/feedback"
	"cityflow/app/task/domain/rider"
	taskdomain "cityflow/app/task/domain/task"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/authn"

	"github.com/zeromicro/go-zero/core/logx"
)

func jsonReply(value any) (*task.JsonReply, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return &task.JsonReply{BodyJson: data}, nil
}

type riderBase struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func newRiderBase(ctx context.Context, s *svc.ServiceContext) riderBase {
	return riderBase{ctx: ctx, svcCtx: s, Logger: logx.WithContext(ctx)}
}

type GetMeLogic struct{ riderBase }

func NewGetMeLogic(ctx context.Context, s *svc.ServiceContext) *GetMeLogic {
	return &GetMeLogic{newRiderBase(ctx, s)}
}
func (l *GetMeLogic) GetMe(in *task.IdRequest) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleRider, c.Role); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Riders.Profile(l.ctx, c.Subject)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type UpdateMeLogic struct{ riderBase }

func NewUpdateMeLogic(ctx context.Context, s *svc.ServiceContext) *UpdateMeLogic {
	return &UpdateMeLogic{newRiderBase(ctx, s)}
}
func (l *UpdateMeLogic) UpdateMe(in *task.IdBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleRider, c.Role); err != nil {
		return nil, err
	}
	var req rider.UpdateRequest
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Riders.Update(l.ctx, c.Subject, req)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type SetMyStatusLogic struct{ riderBase }

func NewSetMyStatusLogic(ctx context.Context, s *svc.ServiceContext) *SetMyStatusLogic {
	return &SetMyStatusLogic{newRiderBase(ctx, s)}
}
func (l *SetMyStatusLogic) SetMyStatus(in *task.IdBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleRider, c.Role); err != nil {
		return nil, err
	}
	var req rider.AvailabilityRequest
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Riders.SetAvailability(l.ctx, c.Subject, req.Status)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ArchiveMeLogic struct{ riderBase }

func NewArchiveMeLogic(ctx context.Context, s *svc.ServiceContext) *ArchiveMeLogic {
	return &ArchiveMeLogic{newRiderBase(ctx, s)}
}
func (l *ArchiveMeLogic) ArchiveMe(in *task.IdRequest) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleRider, c.Role); err != nil {
		return nil, err
	}
	if err := l.svcCtx.Riders.Archive(l.ctx, c.Subject); err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(map[string]bool{"ok": true})
}

type ListOpenTasksLogic struct{ riderBase }

func NewListOpenTasksLogic(ctx context.Context, s *svc.ServiceContext) *ListOpenTasksLogic {
	return &ListOpenTasksLogic{newRiderBase(ctx, s)}
}
func (l *ListOpenTasksLogic) ListOpenTasks(in *task.JsonBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleRider, c.Role); err != nil {
		return nil, err
	}
	var q struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
		Limit     int32    `json:"limit"`
		Offset    int32    `json:"offset"`
	}
	if len(in.GetBodyJson()) > 0 {
		if err := decodeBody(in.GetBodyJson(), &q); err != nil {
			return nil, grpcTaskError(err)
		}
	}
	page, err := pageFromProto(int64(q.Limit), int64(q.Offset))
	if err != nil {
		return nil, grpcTaskError(err)
	}
	var point *location.Point
	if q.Latitude != nil && q.Longitude != nil {
		point = &location.Point{Latitude: *q.Latitude, Longitude: *q.Longitude}
	}
	out, err := l.svcCtx.Riders.ListOpenTasks(l.ctx, c.Subject, point, page)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ClaimWithRulesLogic struct{ riderBase }

func NewClaimWithRulesLogic(ctx context.Context, s *svc.ServiceContext) *ClaimWithRulesLogic {
	return &ClaimWithRulesLogic{newRiderBase(ctx, s)}
}
func (l *ClaimWithRulesLogic) ClaimWithRules(in *task.ClaimRequest) (*task.TaskReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleRider, c.Role); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Riders.Claim(l.ctx, in.GetId(), c.Subject)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	data, err := encodeBody(out)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return &task.TaskReply{BodyJson: data}, nil
}

type CreateIncidentLogic struct{ riderBase }

func NewCreateIncidentLogic(ctx context.Context, s *svc.ServiceContext) *CreateIncidentLogic {
	return &CreateIncidentLogic{newRiderBase(ctx, s)}
}
func (l *CreateIncidentLogic) CreateIncident(in *task.IdBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var req taskdomain.IncidentRequest
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Tasks.CreateIncident(l.ctx, in.GetId(), c.Subject, c.Role, req)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ResolveIncidentLogic struct{ riderBase }

func NewResolveIncidentLogic(ctx context.Context, s *svc.ServiceContext) *ResolveIncidentLogic {
	return &ResolveIncidentLogic{newRiderBase(ctx, s)}
}
func (l *ResolveIncidentLogic) ResolveIncident(in *task.ResolveIncidentRequest) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	var req struct {
		Resolution string `json:"resolution"`
		Cancel     bool   `json:"cancel"`
	}
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Tasks.ResolveIncident(l.ctx, in.GetId(), in.GetIncidentId(), c.Subject, req.Resolution, req.Cancel)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ListIncidentsLogic struct{ riderBase }

func NewListIncidentsLogic(ctx context.Context, s *svc.ServiceContext) *ListIncidentsLogic {
	return &ListIncidentsLogic{newRiderBase(ctx, s)}
}
func (l *ListIncidentsLogic) ListIncidents(in *task.ListQuery) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	page, err := pageFromProto(int64(in.GetLimit()), int64(in.GetOffset()))
	if err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Tasks.ListIncidentsForPlatform(l.ctx, in.GetStatus(), page)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(map[string]any{"data": out, "limit": page.Limit, "offset": page.Offset})
}

type TaskRiderLogic struct{ riderBase }

func NewTaskRiderLogic(ctx context.Context, s *svc.ServiceContext) *TaskRiderLogic {
	return &TaskRiderLogic{newRiderBase(ctx, s)}
}
func (l *TaskRiderLogic) TaskRider(in *task.IdRequest) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Feedback.TaskRider(l.ctx, in.GetId(), c.Subject, c.Role)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type CreateRiderFeedbackLogic struct{ riderBase }

func NewCreateRiderFeedbackLogic(ctx context.Context, s *svc.ServiceContext) *CreateRiderFeedbackLogic {
	return &CreateRiderFeedbackLogic{newRiderBase(ctx, s)}
}
func (l *CreateRiderFeedbackLogic) CreateRiderFeedback(in *task.IdBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var req struct {
		Kind      string   `json:"kind"`
		Rating    int      `json:"rating"`
		Comment   string   `json:"comment"`
		AmountFen int64    `json:"amount_fen"`
		Message   string   `json:"message"`
		Reason    string   `json:"reason"`
		Details   string   `json:"details"`
		Images    []string `json:"images"`
	}
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	var out any
	switch req.Kind {
	case "REVIEW":
		out, err = l.svcCtx.Feedback.CreateReview(l.ctx, in.GetId(), c.Subject, feedback.ReviewRequest{Rating: req.Rating, Comment: req.Comment})
	case "TIP":
		out, err = l.svcCtx.Feedback.CreateTip(l.ctx, in.GetId(), c.Subject, feedback.TipRequest{AmountFen: req.AmountFen, Message: req.Message})
	case "REPORT":
		out, err = l.svcCtx.Feedback.CreateReport(l.ctx, in.GetId(), c.Subject, feedback.ReportRequest{Reason: req.Reason, Details: req.Details, Images: req.Images})
	default:
		return nil, grpcTaskError(errInvalidKind)
	}
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type CallRiderLogic struct{ riderBase }

func NewCallRiderLogic(ctx context.Context, s *svc.ServiceContext) *CallRiderLogic {
	return &CallRiderLogic{newRiderBase(ctx, s)}
}
func (l *CallRiderLogic) CallRider(in *task.IdRequest) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Feedback.CallRider(l.ctx, in.GetId(), c.Subject)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type PublicRiderLogic struct{ riderBase }

func NewPublicRiderLogic(ctx context.Context, s *svc.ServiceContext) *PublicRiderLogic {
	return &PublicRiderLogic{newRiderBase(ctx, s)}
}
func (l *PublicRiderLogic) PublicRider(in *task.IdRequest) (*task.JsonReply, error) {
	claimsValue, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Feedback.PublicRider(l.ctx, in.GetId(), claimsValue.Subject)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ListRidersLogic struct{ riderBase }

func NewListRidersLogic(ctx context.Context, s *svc.ServiceContext) *ListRidersLogic {
	return &ListRidersLogic{newRiderBase(ctx, s)}
}
func (l *ListRidersLogic) ListRiders(in *task.ListQuery) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	page, err := pageFromProto(int64(in.GetLimit()), int64(in.GetOffset()))
	if err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Riders.List(l.ctx, in.GetStatus(), page)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(map[string]any{"data": out, "limit": page.Limit, "offset": page.Offset})
}

type CreateRiderLogic struct{ riderBase }

func NewCreateRiderLogic(ctx context.Context, s *svc.ServiceContext) *CreateRiderLogic {
	return &CreateRiderLogic{newRiderBase(ctx, s)}
}
func (l *CreateRiderLogic) CreateRider(in *task.IdBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	var req rider.UpdateRequest
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Riders.Create(l.ctx, in.GetId(), req)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type GetRiderAdminLogic struct{ riderBase }

func NewGetRiderAdminLogic(ctx context.Context, s *svc.ServiceContext) *GetRiderAdminLogic {
	return &GetRiderAdminLogic{newRiderBase(ctx, s)}
}
func (l *GetRiderAdminLogic) GetRiderAdmin(in *task.IdRequest) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Riders.Profile(l.ctx, in.GetId())
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type UpdateRiderLogic struct{ riderBase }

func NewUpdateRiderLogic(ctx context.Context, s *svc.ServiceContext) *UpdateRiderLogic {
	return &UpdateRiderLogic{newRiderBase(ctx, s)}
}
func (l *UpdateRiderLogic) UpdateRider(in *task.IdBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	var req rider.UpdateRequest
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Riders.Update(l.ctx, in.GetId(), req)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ArchiveRiderLogic struct{ riderBase }

func NewArchiveRiderLogic(ctx context.Context, s *svc.ServiceContext) *ArchiveRiderLogic {
	return &ArchiveRiderLogic{newRiderBase(ctx, s)}
}
func (l *ArchiveRiderLogic) ArchiveRider(in *task.IdRequest) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	if err := l.svcCtx.Riders.Archive(l.ctx, in.GetId()); err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(map[string]bool{"ok": true})
}

type ListRiderReportsLogic struct{ riderBase }

func NewListRiderReportsLogic(ctx context.Context, s *svc.ServiceContext) *ListRiderReportsLogic {
	return &ListRiderReportsLogic{newRiderBase(ctx, s)}
}
func (l *ListRiderReportsLogic) ListRiderReports(in *task.ListQuery) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	page, err := pageFromProto(int64(in.GetLimit()), int64(in.GetOffset()))
	if err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Feedback.Reports(l.ctx, in.GetStatus(), page)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(map[string]any{"data": out, "limit": page.Limit, "offset": page.Offset})
}

type ResolveRiderReportLogic struct{ riderBase }

func NewResolveRiderReportLogic(ctx context.Context, s *svc.ServiceContext) *ResolveRiderReportLogic {
	return &ResolveRiderReportLogic{newRiderBase(ctx, s)}
}
func (l *ResolveRiderReportLogic) ResolveRiderReport(in *task.IdBody) (*task.JsonReply, error) {
	c, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, c.Role); err != nil {
		return nil, err
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Feedback.ResolveReport(l.ctx, in.GetId(), c.Subject, req.Status, req.Note)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type GetTaskInternalLogic struct{ riderBase }

func NewGetTaskInternalLogic(ctx context.Context, s *svc.ServiceContext) *GetTaskInternalLogic {
	return &GetTaskInternalLogic{newRiderBase(ctx, s)}
}
func (l *GetTaskInternalLogic) GetTaskInternal(in *task.IdRequest) (*task.JsonReply, error) {
	if _, err := claims(l.ctx); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Tasks.Get(l.ctx, in.GetId())
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ListTasksInternalLogic struct{ riderBase }

func NewListTasksInternalLogic(ctx context.Context, s *svc.ServiceContext) *ListTasksInternalLogic {
	return &ListTasksInternalLogic{newRiderBase(ctx, s)}
}
func (l *ListTasksInternalLogic) ListTasksInternal(in *task.ListQuery) (*task.JsonReply, error) {
	if !authn.IsPaymentWorker(l.ctx) {
		if _, err := claims(l.ctx); err != nil {
			return nil, err
		}
	} else if in.GetStatus() != taskdomain.StatusCompleted {
		return nil, grpcTaskError(domain.ErrForbidden)
	}
	page, err := pageFromProto(int64(in.GetLimit()), int64(in.GetOffset()))
	if err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Tasks.ListAll(l.ctx, in.GetStatus(), page)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type GetRiderByUserLogic struct{ riderBase }

func NewGetRiderByUserLogic(ctx context.Context, s *svc.ServiceContext) *GetRiderByUserLogic {
	return &GetRiderByUserLogic{newRiderBase(ctx, s)}
}
func (l *GetRiderByUserLogic) GetRiderByUser(in *task.IdRequest) (*task.JsonReply, error) {
	if _, err := claims(l.ctx); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Riders.Profile(l.ctx, in.GetId())
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type GetRiderByIDLogic struct{ riderBase }

func NewGetRiderByIDLogic(ctx context.Context, s *svc.ServiceContext) *GetRiderByIDLogic {
	return &GetRiderByIDLogic{newRiderBase(ctx, s)}
}
func (l *GetRiderByIDLogic) GetRiderByID(in *task.IdRequest) (*task.JsonReply, error) {
	if _, err := claims(l.ctx); err != nil {
		return nil, err
	}
	if l.svcCtx.RiderRepo == nil {
		return nil, grpcTaskError(errUnavailable)
	}
	out, err := l.svcCtx.RiderRepo.GetRider(l.ctx, in.GetId())
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ProvisionRiderLogic struct{ riderBase }

func NewProvisionRiderLogic(ctx context.Context, s *svc.ServiceContext) *ProvisionRiderLogic {
	return &ProvisionRiderLogic{newRiderBase(ctx, s)}
}
func (l *ProvisionRiderLogic) ProvisionRider(in *task.IdBody) (*task.JsonReply, error) {
	if _, err := claims(l.ctx); err != nil {
		return nil, err
	}
	var req rider.UpdateRequest
	if err := decodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, grpcTaskError(err)
	}
	out, err := l.svcCtx.Riders.Create(l.ctx, in.GetId(), req)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}
