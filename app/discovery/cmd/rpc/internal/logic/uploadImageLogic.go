package logic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/common/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

const maxMerchantImageBytes = 5 << 20

var merchantImageExt = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

type UploadImageLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUploadImageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadImageLogic {
	return &UploadImageLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *UploadImageLogic) UploadImage(in *pb.UploadRequest) (*pb.JsonReply, error) {
	if _, err := currentUser(l.ctx); err != nil {
		return nil, err
	}
	data := in.GetData()
	if len(data) == 0 || len(data) > maxMerchantImageBytes {
		return nil, grpcError(fmt.Errorf("%w: 图片大小需在 5 MB 以内", domain.ErrInvalidRequest))
	}
	mime := http.DetectContentType(data)
	ext, ok := merchantImageExt[mime]
	if !ok {
		return nil, grpcError(fmt.Errorf("%w: 仅支持 JPEG、PNG 或 WebP 图片", domain.ErrInvalidRequest))
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, grpcError(fmt.Errorf("%w: 图片保存失败", domain.ErrUnavailable))
	}
	filename := hex.EncodeToString(key[:]) + ext
	if err := os.MkdirAll(l.svcCtx.UploadDir, 0o755); err != nil {
		return nil, grpcError(fmt.Errorf("%w: 图片目录不可写", domain.ErrUnavailable))
	}
	if err := os.WriteFile(filepath.Join(l.svcCtx.UploadDir, filename), data, 0o644); err != nil {
		return nil, grpcError(fmt.Errorf("%w: 图片保存失败", domain.ErrUnavailable))
	}
	return jsonReply(map[string]string{"url": "/uploads/" + filename})
}
