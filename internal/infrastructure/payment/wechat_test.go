package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	paymentdomain "cityflow/internal/domain/payment"
)

const testAPIv3Key = "0123456789abcdef0123456789abcdef"

func encryptResource(t *testing.T, key, nonce, associatedData, plaintext string) string {
	t.Helper()
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	sealed := gcm.Seal(nil, []byte(nonce), []byte(plaintext), []byte(associatedData))
	return base64.StdEncoding.EncodeToString(sealed)
}

func wechatTestConfig(merchant, platform *rsa.PrivateKey) WechatConfig {
	return WechatConfig{
		AppID: "wx1234567890abcdef", MchID: "1900000109", SerialNumber: "SERIALNUMBER",
		APIv3Key: testAPIv3Key, NotifyURL: "https://merchant.example/api/v1/payment-notify/wechat",
		PrivateKey: merchant, PlatformPublicKey: &platform.PublicKey,
	}
}

func wechatNotifyBody(t *testing.T, plaintext string) (string, string, string) {
	t.Helper()
	nonce := "123456789012"
	ciphertext := encryptResource(t, testAPIv3Key, nonce, "transaction", plaintext)
	body := fmt.Sprintf(`{"id":"EV-1","event_type":"TRANSACTION.SUCCESS","resource":{"algorithm":"AEAD_AES_256_GCM","ciphertext":%q,"nonce":%q,"associated_data":"transaction"}}`, ciphertext, nonce)
	return body, nonce, ciphertext
}

func TestWechatChargeNotifyVerifiesSignatureAndDecrypts(t *testing.T) {
	merchantKey := newTestRSAKey(t)
	platformKey := newTestRSAKey(t)
	gateway := NewWechat(wechatTestConfig(merchantKey, platformKey))

	plaintext := `{"appid":"wx1234567890abcdef","mchid":"1900000109","out_trade_no":"CFORDER123","transaction_id":"4200001234","trade_state":"SUCCESS","success_time":"2026-11-14T10:00:00+08:00","amount":{"total":1234,"currency":"CNY"}}`
	body, nonce, _ := wechatNotifyBody(t, plaintext)
	timestamp := "1763000000"
	message := timestamp + "\n" + nonce + "\n" + body + "\n"
	signature, err := SignSHA256(platformKey, message)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{
		"wechatpay-timestamp": timestamp, "wechatpay-nonce": nonce, "wechatpay-signature": signature,
	}
	notification, err := gateway.ParseNotify(context.Background(), headers, []byte(body))
	if err != nil {
		t.Fatalf("parse notify: %v", err)
	}
	if notification.Status != paymentdomain.PaymentPaid || notification.AmountFen != 1234 ||
		notification.OrderNo != "CFORDER123" || notification.TradeID != "4200001234" {
		t.Fatalf("unexpected notification: %+v", notification)
	}

	bodyWithMore := strings.Replace(body, `"EV-1"`, `"EV-2"`, 1)
	if _, err := gateway.ParseNotify(context.Background(), headers, []byte(bodyWithMore)); err == nil {
		t.Fatal("a modified body must fail the WeChat signature check")
	}
}

func TestWechatPayoutNotify(t *testing.T) {
	merchantKey := newTestRSAKey(t)
	platformKey := newTestRSAKey(t)
	gateway := NewWechat(wechatTestConfig(merchantKey, platformKey))
	plaintext := `{"appid":"wx1234567890abcdef","mchid":"1900000109","out_bill_no":"ST123","transfer_bill_no":"1330000071100999991182020050700019480","state":"SUCCESS"}`
	body, nonce, _ := wechatNotifyBody(t, plaintext)
	timestamp := "1763000000"
	signature, _ := SignSHA256(platformKey, timestamp+"\n"+nonce+"\n"+body+"\n")
	headers := map[string]string{
		"wechatpay-timestamp": timestamp, "wechatpay-nonce": nonce, "wechatpay-signature": signature,
	}
	notification, err := gateway.ParseNotify(context.Background(), headers, []byte(body))
	if err != nil {
		t.Fatalf("parse payout notify: %v", err)
	}
	if notification.Kind != paymentdomain.NotifyKindPayout || notification.BillNo != "ST123" ||
		notification.Status != paymentdomain.SettlementPaid {
		t.Fatalf("unexpected payout notification: %+v", notification)
	}
}

func TestWechatPrecreateSignsAPIv3Request(t *testing.T) {
	merchantKey := newTestRSAKey(t)
	platformKey := newTestRSAKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		authorization := r.Header.Get("Authorization")
		prefix := "WECHATPAY2-SHA256-RSA2048 "
		if !strings.HasPrefix(authorization, prefix) {
			http.Error(w, "bad authorization scheme", http.StatusUnauthorized)
			return
		}
		pairs := map[string]string{}
		for _, part := range strings.Split(strings.TrimPrefix(authorization, prefix), ",") {
			keyValue := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(keyValue) == 2 {
				pairs[keyValue[0]] = strings.Trim(keyValue[1], `"`)
			}
		}
		message := r.Method + "\n" + r.URL.RequestURI() + "\n" + pairs["timestamp"] + "\n" +
			pairs["nonce_str"] + "\n" + string(body) + "\n"
		if err := VerifySHA256(&merchantKey.PublicKey, message, pairs["signature"]); err != nil {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		if pairs["mchid"] != "1900000109" || pairs["serial_no"] != "SERIALNUMBER" {
			http.Error(w, "unexpected identity", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		responseBody := `{"code_url":"weixin://wxpay/bizpayurl?pr=abc"}`
		responseTimestamp := "1763000001"
		responseNonce := "response-nonce"
		responseSignature, _ := SignSHA256(platformKey, responseTimestamp+"\n"+responseNonce+"\n"+responseBody+"\n")
		w.Header().Set("Wechatpay-Timestamp", responseTimestamp)
		w.Header().Set("Wechatpay-Nonce", responseNonce)
		w.Header().Set("Wechatpay-Signature", responseSignature)
		fmt.Fprint(w, responseBody)
	}))
	defer server.Close()

	config := wechatTestConfig(merchantKey, platformKey)
	config.BaseURL = server.URL
	gateway := NewWechat(config)
	charge, err := gateway.Precreate(context.Background(), paymentdomain.ChargeRequest{
		OrderNo: "CFORDER123", AmountFen: 1234, Subject: "测试订单", ExpiresAt: time.Now().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("precreate: %v", err)
	}
	if charge.QRCode != "weixin://wxpay/bizpayurl?pr=abc" {
		t.Fatalf("unexpected charge: %+v", charge)
	}
}

func TestWechatRefundNotify(t *testing.T) {
	merchantKey := newTestRSAKey(t)
	platformKey := newTestRSAKey(t)
	gateway := NewWechat(wechatTestConfig(merchantKey, platformKey))
	plaintext := `{"appid":"wx1234567890abcdef","mchid":"1900000109","out_trade_no":"CFORDER123","transaction_id":"4200001234","out_refund_no":"RF-CFORDER123","refund_id":"50300000002025111400000000001","refund_status":"SUCCESS","amount":{"total":1234,"refund":1234}}`
	body, nonce, _ := wechatNotifyBody(t, plaintext)
	timestamp := "1763000000"
	signature, _ := SignSHA256(platformKey, timestamp+"\n"+nonce+"\n"+body+"\n")
	headers := map[string]string{
		"wechatpay-timestamp": timestamp, "wechatpay-nonce": nonce, "wechatpay-signature": signature,
	}
	notification, err := gateway.ParseNotify(context.Background(), headers, []byte(body))
	if err != nil {
		t.Fatalf("parse refund notify: %v", err)
	}
	if notification.Kind != paymentdomain.NotifyKindRefund || notification.Status != paymentdomain.PaymentRefunded ||
		notification.RefundNo != "RF-CFORDER123" || notification.OrderNo != "CFORDER123" || notification.AmountFen != 1234 {
		t.Fatalf("unexpected refund notification: %+v", notification)
	}
}

func TestWechatReconciliationEndpoints(t *testing.T) {
	merchantKey := newTestRSAKey(t)
	platformKey := newTestRSAKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/refund/domestic/refunds/RF-CFORDER123":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"refund_id":"50300000002025111400000000001","status":"SUCCESS"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v3/pay/transactions/out-trade-no/CFORDER123/close":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := wechatTestConfig(merchantKey, platformKey)
	config.BaseURL = server.URL
	gateway := NewWechat(config)
	refund, err := gateway.QueryRefund(context.Background(), "CFORDER123", "RF-CFORDER123")
	if err != nil || refund.Status != paymentdomain.PaymentRefunded {
		t.Fatalf("query refund: %+v %v", refund, err)
	}
	if err := gateway.Close(context.Background(), "CFORDER123"); err != nil {
		t.Fatalf("close order: %v", err)
	}
}

func TestWechatWithoutCredentialsIsNotConfigured(t *testing.T) {
	gateway := NewWechat(WechatConfig{})
	if gateway.Configured() {
		t.Fatal("empty config must not report configured")
	}
	if _, err := gateway.ParseNotify(context.Background(), nil, []byte("{}")); err == nil {
		t.Fatal("notify without credentials must fail")
	}
}
