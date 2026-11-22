package logic

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/common/contract"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ListHotMerchantsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListHotMerchantsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListHotMerchantsLogic {
	return &ListHotMerchantsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

type hotMerchantsBody struct {
	Category   string   `json:"category"`
	Month      string   `json:"month"`
	Limit      int      `json:"limit"`
	Rebuild    bool     `json:"rebuild"`
	Categories []string `json:"categories"`
}

func (l *ListHotMerchantsLogic) ListHotMerchants(in *pb.JsonBody) (*pb.JsonReply, error) {
	var body hotMerchantsBody
	if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid hot merchant body")
	}
	if body.Rebuild {
		if !l.internalJobAuthorized() {
			return nil, status.Error(codes.PermissionDenied, "rank rebuild is restricted to the internal scheduler")
		}
		result, err := l.svcCtx.LikeSvc.Rebuild(l.ctx, body.Categories, body.Limit)
		if err != nil {
			return nil, reactionError(err)
		}
		return &pb.JsonReply{BodyJson: mustReactionJSON(result)}, nil
	}
	if month := strings.TrimSpace(body.Month); month != "" {
		result, err := l.svcCtx.LikeSvc.History(l.ctx, month, body.Category, body.Limit)
		if err != nil {
			return nil, reactionError(err)
		}
		return &pb.JsonReply{BodyJson: mustReactionJSON(result)}, nil
	}
	result, err := l.svcCtx.LikeSvc.Hot(l.ctx, body.Category, body.Limit)
	if err != nil {
		return nil, reactionError(err)
	}
	return &pb.JsonReply{BodyJson: mustReactionJSON(result)}, nil
}

func (l *ListHotMerchantsLogic) internalJobAuthorized() bool {
	return internalJobAuthorized(l.ctx, l.svcCtx.InternalJobToken)
}

func internalJobAuthorized(ctx context.Context, want string) bool {
	if want == "" {
		return false
	}
	values := metadata.ValueFromIncomingContext(ctx, contract.InternalJobTokenHeader)
	if len(values) == 0 {
		return false
	}
	got := values[0]
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
