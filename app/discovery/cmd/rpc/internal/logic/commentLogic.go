package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/app/discovery/domain/comment"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreatePlaceCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreatePlaceCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatePlaceCommentLogic {
	return &CreatePlaceCommentLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CreatePlaceCommentLogic) CreatePlaceComment(in *pb.CommentRequest) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	var request comment.CreateRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	if in.GetParentId() != "" {
		request.ParentID = in.GetParentId()
	}
	out, err := l.svcCtx.CommentSvc.Create(l.ctx, in.GetReviewId(), userID, request)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(out)
}

type ListPlaceCommentsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListPlaceCommentsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListPlaceCommentsLogic {
	return &ListPlaceCommentsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListPlaceCommentsLogic) ListPlaceComments(in *pb.CommentListRequest) (*pb.JsonReply, error) {
	out, err := l.svcCtx.CommentSvc.ListTop(l.ctx, in.GetReviewId(), optionalUser(l.ctx), in.GetSort(), in.GetCursor(), int(in.GetLimit()))
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(out)
}

type ListPlaceCommentThreadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListPlaceCommentThreadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListPlaceCommentThreadLogic {
	return &ListPlaceCommentThreadLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListPlaceCommentThreadLogic) ListPlaceCommentThread(in *pb.CommentThreadRequest) (*pb.JsonReply, error) {
	out, err := l.svcCtx.CommentSvc.Thread(l.ctx, in.GetReviewId(), optionalUser(l.ctx), in.GetRootId(), int(in.GetLimit()))
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"items": out})
}

type HidePlaceCommentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewHidePlaceCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HidePlaceCommentLogic {
	return &HidePlaceCommentLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *HidePlaceCommentLogic) HidePlaceComment(in *pb.CommentRequest) (*pb.JsonReply, error) {
	if _, err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	if err := l.svcCtx.CommentSvc.Hide(l.ctx, in.GetReviewId(), in.GetParentId()); err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]bool{"hidden": true})
}
