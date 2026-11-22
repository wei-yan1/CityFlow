package logic

import (
	"context"
	"encoding/json"
	"strings"

	"cityflow/app/task/cmd/rpc/internal/svc"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	"cityflow/internal/common/contract"
	"cityflow/internal/platform/rpckit"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var taskReactionTargets = map[string]bool{
	contract.TargetRiderReview:  true,
	contract.TargetRiderComment: true,
}

type taskSetReactionBody struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Kind       string `json:"kind"`
	Active     bool   `json:"active"`
}

type SetReactionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSetReactionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SetReactionLogic {
	return &SetReactionLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SetReactionLogic) SetReaction(in *taskpb.JsonBody) (*taskpb.JsonReply, error) {
	claims, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var body taskSetReactionBody
	if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reaction body")
	}
	if !taskReactionTargets[body.TargetType] {
		return nil, status.Error(codes.InvalidArgument, "target_type is not owned by task")
	}
	if !validTaskReactionKind(body.Kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported kind")
	}
	body.TargetID = strings.TrimSpace(body.TargetID)
	if body.TargetID == "" || len(body.TargetID) > 128 {
		return nil, status.Error(codes.InvalidArgument, "invalid target_id")
	}
	count, err := l.svcCtx.Reactions.Set(l.ctx, body.TargetType, body.TargetID, claims.Subject, body.Kind, body.Active)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "reaction store unavailable")
	}
	return &taskpb.JsonReply{BodyJson: taskReactionJSON(map[string]any{"active": body.Active, "count": count})}, nil
}

type taskReactionStateBody struct {
	TargetType string   `json:"target_type"`
	Kind       string   `json:"kind"`
	TargetIDs  []string `json:"target_ids"`
}

type GetReactionStateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetReactionStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetReactionStateLogic {
	return &GetReactionStateLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GetReactionStateLogic) GetReactionState(in *taskpb.JsonBody) (*taskpb.JsonReply, error) {
	userID := ""
	if claims, err := rpckit.Claims(l.ctx); err == nil {
		userID = claims.Subject
	} else if status.Code(err) != codes.Unauthenticated {
		return nil, err
	}
	var body taskReactionStateBody
	if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reaction state body")
	}
	if !taskReactionTargets[body.TargetType] || !validTaskReactionKind(body.Kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported target_type or kind")
	}
	ids := taskReactionIDs(body.TargetIDs)
	states, err := l.svcCtx.Reactions.States(l.ctx, body.TargetType, ids, userID, body.Kind)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "reaction store unavailable")
	}
	items := make([]map[string]any, 0, len(states))
	for _, state := range states {
		items = append(items, map[string]any{
			"target_id": state.TargetID, "count": state.Count, "liked": state.Liked,
		})
	}
	return &taskpb.JsonReply{BodyJson: taskReactionJSON(map[string]any{"items": items})}, nil
}

type taskListMineBody struct {
	Kind       string `json:"kind"`
	TargetType string `json:"target_type"`
	Cursor     string `json:"cursor"`
	Limit      int    `json:"limit"`
}

type ListMineReactionsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMineReactionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMineReactionsLogic {
	return &ListMineReactionsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListMineReactionsLogic) ListMineReactions(in *taskpb.JsonBody) (*taskpb.JsonReply, error) {
	claims, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var body taskListMineBody
	if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reaction list body")
	}
	if !taskReactionTargets[body.TargetType] || !validTaskReactionKind(body.Kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported target_type or kind")
	}
	records, next, err := l.svcCtx.Reactions.ListMine(l.ctx, claims.Subject, body.Kind, body.TargetType, body.Cursor, body.Limit)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "reaction store unavailable")
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"target_id": record.TargetID, "created_at_ms": record.CreatedAt.UnixMilli(),
		})
	}
	return &taskpb.JsonReply{BodyJson: taskReactionJSON(map[string]any{"items": items, "next_cursor": next})}, nil
}

func validTaskReactionKind(kind string) bool {
	return kind == contract.KindLike || kind == contract.KindFavorite
}

func taskReactionIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == 50 {
			break
		}
	}
	return out
}

func taskReactionJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}
