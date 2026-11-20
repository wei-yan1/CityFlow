package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	paymentdomain "cityflow/internal/domain/payment"
)

func newTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func TestAlipayNotifyVerifiesSignatureAndAmount(t *testing.T) {
	key := newTestRSAKey(t)
	gateway := NewAlipay(AlipayConfig{
		AppID: "2021000000000000", NotifyURL: "https://merchant.example/api/v1/payment-notify/alipay",
		PrivateKey: key, PublicKey: &key.PublicKey,
	})
	form := url.Values{}
	form.Set("app_id", "2021000000000000")
	form.Set("out_trade_no", "CFORDER123")
	form.Set("trade_no", "2026111422001400000001")
	form.Set("total_amount", "12.34")
	form.Set("trade_status", "TRADE_SUCCESS")
	form.Set("gmt_payment", "2026-11-14 10:00:00")
	signature, err := SignSHA256(key, alipayCanonical(form))
	if err != nil {
		t.Fatal(err)
	}
	form.Set("sign", signature)

	notification, err := gateway.ParseNotify(context.Background(), nil, []byte(form.Encode()))
	if err != nil {
		t.Fatalf("parse notify: %v", err)
	}
	if notification.Status != paymentdomain.PaymentPaid || notification.AmountFen != 1234 || notification.OrderNo != "CFORDER123" {
		t.Fatalf("unexpected notification: %+v", notification)
	}
	if notification.PaidAt == nil || notification.PaidAt.Location() != time.UTC {
		t.Fatalf("paid_at should be normalized to UTC: %v", notification.PaidAt)
	}

	tampered := url.Values{}
	for key, values := range form {
		tampered[key] = values
	}
	tampered.Set("total_amount", "0.01")
	if _, err := gateway.ParseNotify(context.Background(), nil, []byte(tampered.Encode())); err == nil {
		t.Fatal("a tampered amount must fail signature verification")
	}
}

func TestAlipayPrecreateSignsRequestAndVerifiesResponse(t *testing.T) {
	key := newTestRSAKey(t)
	orderNo := "CFTESTORDER0001"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		form := r.PostForm
		signature := form.Get("sign")
		form.Del("sign")
		if err := VerifySHA256(&key.PublicKey, alipayCanonical(form), signature); err != nil {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		if form.Get("method") != "alipay.trade.precreate" {
			http.Error(w, "unexpected method", http.StatusBadRequest)
			return
		}
		node := fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"qr_code":"https://qr.alipay.com/abc"}`, orderNo)
		nodeSignature, _ := SignSHA256(key, node)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"alipay_trade_precreate_response":%s,"sign":%q}`, node, nodeSignature)
	}))
	defer server.Close()

	gateway := NewAlipay(AlipayConfig{
		AppID: "2021000000000000", GatewayURL: server.URL,
		NotifyURL:  "https://merchant.example/api/v1/payment-notify/alipay",
		PrivateKey: key, PublicKey: &key.PublicKey,
	})
	charge, err := gateway.Precreate(context.Background(), paymentdomain.ChargeRequest{
		OrderNo: orderNo, AmountFen: 1234, Subject: "测试订单", ExpiresAt: time.Now().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("precreate: %v", err)
	}
	if charge.QRCode != "https://qr.alipay.com/abc" || charge.OrderNo != orderNo {
		t.Fatalf("unexpected charge: %+v", charge)
	}
}

func TestAlipayWithoutCredentialsIsNotConfigured(t *testing.T) {
	gateway := NewAlipay(AlipayConfig{})
	if gateway.Configured() {
		t.Fatal("empty config must not report configured")
	}
	_, err := gateway.Precreate(context.Background(), paymentdomain.ChargeRequest{OrderNo: "CF1", AmountFen: 1})
	if err == nil {
		t.Fatal("precreate without credentials must fail")
	}
}
