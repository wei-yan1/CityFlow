package repository

import "context"

type MerchantMedia interface {
	GetMerchantMedia(context.Context, string) (string, []string, error)
	SaveMerchantMedia(context.Context, string, string, []string) error
}
