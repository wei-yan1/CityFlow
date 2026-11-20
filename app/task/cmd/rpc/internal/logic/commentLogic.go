package logic

import (
	"context"

	"cityflow/app/task/cmd/rpc/internal/svc"
	"cityflow/app/task/cmd/rpc/pb"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/comment"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateRiderCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateRiderCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateRiderCommentLogic {
	return &CreateRiderCommentLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CreateRiderCommentLogic) CreateRiderComment(in *task.CommentRequest) (*task.JsonReply, error) {
	claimsValue, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var request comment.CreateRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcTaskError(err)
	}
	if in.GetParentId() != "" {
		request.ParentID = in.GetParentId()
	}
	// Rider comments are always anonymous in public projections; the service
	// force-flag is the single source of that rule.
	out, err := l.svcCtx.Comments.Create(l.ctx, in.GetFeedbackId(), claimsValue.Subject, request)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ListRiderCommentsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListRiderCommentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRiderCommentsLogic {
	return &ListRiderCommentsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListRiderCommentsLogic) ListRiderComments(in *task.CommentListRequest) (*task.JsonReply, error) {
	claimsValue, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Comments.ListTop(l.ctx, in.GetFeedbackId(), claimsValue.Subject, in.GetCursor(), int(in.GetLimit()))
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(out)
}

type ListRiderCommentThreadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListRiderCommentThreadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRiderCommentThreadLogic {
	return &ListRiderCommentThreadLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListRiderCommentThreadLogic) ListRiderCommentThread(in *task.CommentThreadRequest) (*task.JsonReply, error) {
	claimsValue, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Comments.Thread(l.ctx, in.GetFeedbackId(), claimsValue.Subject, in.GetRootId(), int(in.GetLimit()))
	if err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(map[string]any{"items": out})
}

type HideRiderCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewHideRiderCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HideRiderCommentLogic {
	return &HideRiderCommentLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *HideRiderCommentLogic) HideRiderComment(in *task.CommentRequest) (*task.JsonReply, error) {
	claimsValue, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleAdmin, claimsValue.Role); err != nil {
		return nil, err
	}
	if err := l.svcCtx.Comments.Hide(l.ctx, in.GetFeedbackId(), in.GetParentId()); err != nil {
		return nil, grpcTaskError(err)
	}
	return jsonReply(map[string]bool{"hidden": true})
}
