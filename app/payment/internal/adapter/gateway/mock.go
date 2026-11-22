package payment

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	paymentdomain "cityflow/app/payment/domain/payment"
)

// Mock is the only gateway that works without credentials. It never claims a
// real payment happened: the client must call the explicit mock-pay endpoint,
// which goes through the same state machine as a provider callback.
type Mock struct {
	publicBaseURL string
}

func NewMock(publicBaseURL string) *Mock {
	return &Mock{publicBaseURL: strings.TrimSpace(publicBaseURL)}
}

func (m *Mock) Name() string { return paymentdomain.ProviderMock }

func (m *Mock) Configured() bool { return true }

func (m *Mock) Precreate(_ context.Context, request paymentdomain.ChargeRequest) (*paymentdomain.Charge, error) {
	if request.AmountFen <= 0 {
		return nil, fmt.Errorf("mock payment amount must be positive")
	}
	payload := "cityflow-mock://pay/" + request.OrderNo
	payURL := ""
	if m.publicBaseURL != "" {
		payURL = strings.TrimRight(m.publicBaseURL, "/") + "/pay/mock?order=" + url.QueryEscape(request.OrderNo)
	}
	return &paymentdomain.Charge{
		Provider: paymentdomain.ProviderMock, OrderNo: request.OrderNo,
		QRCode: payload, PayURL: payURL, ExpiresAt: request.ExpiresAt,
	}, nil
}

func (m *Mock) Query(_ context.Context, orderNo string) (*paymentdomain.ChargeState, error) {
	return &paymentdomain.ChargeState{OrderNo: orderNo, Status: paymentdomain.PaymentPending}, nil
}

func (m *Mock) ParseNotify(context.Context, map[string]string, []byte) (*paymentdomain.Notification, error) {
	return nil, errors.New("mock provider does not accept callbacks; use the mock-pay endpoint")
}

func (m *Mock) Transfer(_ context.Context, request paymentdomain.PayoutRequest) (*paymentdomain.Payout, error) {
	return &paymentdomain.Payout{BillNo: request.BillNo, Status: paymentdomain.SettlementPaid}, nil
}

func (m *Mock) Refund(_ context.Context, request paymentdomain.RefundRequest) (*paymentdomain.Refund, error) {
	return &paymentdomain.Refund{RefundID: request.RefundNo, Status: paymentdomain.PaymentRefunded}, nil
}

func (m *Mock) Close(context.Context, string) error { return nil }

func (m *Mock) QueryRefund(_ context.Context, _, refundNo string) (*paymentdomain.Refund, error) {
	return &paymentdomain.Refund{RefundID: refundNo, Status: paymentdomain.PaymentRefunded}, nil
}

func (m *Mock) QueryPayout(_ context.Context, billNo string) (*paymentdomain.Payout, error) {
	return &paymentdomain.Payout{BillNo: billNo, Status: paymentdomain.SettlementPaid}, nil
}

var _ paymentdomain.Gateway = (*Mock)(nil)
var _ paymentdomain.Closer = (*Mock)(nil)
var _ paymentdomain.RefundQuerier = (*Mock)(nil)
var _ paymentdomain.PayoutQuerier = (*Mock)(nil)
