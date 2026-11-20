package server

import (
	"context"

	"cityflow/app/reaction/cmd/rpc/internal/logic"
	"cityflow/app/reaction/cmd/rpc/internal/svc"
	"cityflow/app/reaction/cmd/rpc/pb"
)

type ReactionServer struct {
	svcCtx *svc.ServiceContext
	pb.UnimplementedReactionServer
}

func NewReactionServer(svcCtx *svc.ServiceContext) *ReactionServer {
	return &ReactionServer{svcCtx: svcCtx}
}

func (s *ReactionServer) SetReaction(ctx context.Context, in *pb.SetReactionReq) (*pb.SetReactionResp, error) {
	return logic.NewSetReactionLogic(ctx, s.svcCtx).SetReaction(in)
}
func (s *ReactionServer) GetReactionState(ctx context.Context, in *pb.ReactionStateReq) (*pb.ReactionStateResp, error) {
	return logic.NewGetReactionStateLogic(ctx, s.svcCtx).GetReactionState(in)
}
func (s *ReactionServer) ListMine(ctx context.Context, in *pb.ListMineReq) (*pb.ListMineResp, error) {
	return logic.NewListMineLogic(ctx, s.svcCtx).ListMine(in)
}
func (s *ReactionServer) Health(ctx context.Context, in *pb.HealthReq) (*pb.HealthResp, error) {
	return logic.NewHealthLogic(ctx, s.svcCtx).Health(in)
}
