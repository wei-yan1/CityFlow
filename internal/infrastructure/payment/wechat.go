package payment

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	paymentdomain "cityflow/internal/domain/payment"
)

const wechatDefaultBaseURL = "https://api.mch.weixin.qq.com"

// WechatConfig holds the API v3 merchant credentials. The platform public key
// can be downloaded from 商户平台 > 账户中心 > API安全 > 平台证书 and pasted into
// a file; automatic certificate rotation is intentionally out of scope.
type WechatConfig struct {
	AppID             string
	MchID             string
	SerialNumber      string
	APIv3Key          string
	NotifyURL         string
	BaseURL           string
	TransferSceneID   string
	PrivateKey        *rsa.PrivateKey
	PlatformPublicKey *rsa.PublicKey
	HTTPClient        *http.Client
}

type Wechat struct{ config WechatConfig }

func NewWechat(config WechatConfig) *Wechat {
	if strings.TrimSpace(config.BaseURL) == "" {
		config.BaseURL = wechatDefaultBaseURL
	}
	if strings.TrimSpace(config.TransferSceneID) == "" {
		config.TransferSceneID = "1000"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	return &Wechat{config: config}
}

func (w *Wechat) Name() string { return paymentdomain.ProviderWechat }

func (w *Wechat) Configured() bool {
	return strings.TrimSpace(w.config.AppID) != "" && strings.TrimSpace(w.config.MchID) != "" &&
		strings.TrimSpace(w.config.SerialNumber) != "" && len(w.config.APIv3Key) == 32 &&
		strings.TrimSpace(w.config.NotifyURL) != "" && w.config.PrivateKey != nil &&
		w.config.PlatformPublicKey != nil
}

func (w *Wechat) Precreate(ctx context.Context, request paymentdomain.ChargeRequest) (*paymentdomain.Charge, error) {
	if err := w.requireConfigured(); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"appid":        w.config.AppID,
		"mchid":        w.config.MchID,
		"description":  truncate(request.Subject, 127),
		"out_trade_no": request.OrderNo,
		"notify_url":   w.config.NotifyURL,
		"amount":       map[string]any{"total": request.AmountFen, "currency": paymentdomain.CurrencyCNY},
		"time_expire":  request.ExpiresAt.In(shanghaiZone).Format(time.RFC3339),
	}
	var result struct {
		CodeURL string `json:"code_url"`
	}
	if err := w.do(ctx, http.MethodPost, "/v3/pay/transactions/native", payload, &result); err != nil {
		return nil, err
	}
	if result.CodeURL == "" {
		return nil, errors.New("wechat native precreate returned an empty code_url")
	}
	return &paymentdomain.Charge{
		Provider: paymentdomain.ProviderWechat, OrderNo: request.OrderNo,
		QRCode: result.CodeURL, PayURL: result.CodeURL, ExpiresAt: request.ExpiresAt,
	}, nil
}

func (w *Wechat) Query(ctx context.Context, orderNo string) (*paymentdomain.ChargeState, error) {
	if err := w.requireConfigured(); err != nil {
		return nil, err
	}
	path := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(orderNo) + "?mchid=" + url.QueryEscape(w.config.MchID)
	var result struct {
		OutTradeNo    string `json:"out_trade_no"`
		TradeState    string `json:"trade_state"`
		TransactionID string `json:"transaction_id"`
		SuccessTime   string `json:"success_time"`
		Amount        struct {
			Total int64 `json:"total"`
		} `json:"amount"`
	}
	if err := w.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	state := &paymentdomain.ChargeState{
		OrderNo: firstNonEmpty(result.OutTradeNo, orderNo), TradeID: result.TransactionID, AmountFen: result.Amount.Total,
	}
	switch result.TradeState {
	case "SUCCESS":
		state.Status = paymentdomain.PaymentPaid
		if paidAt, err := time.Parse(time.RFC3339, result.SuccessTime); err == nil {
			paid := paidAt.UTC()
			state.PaidAt = &paid
		}
	case "CLOSED", "REVOKED", "PAYERROR":
		state.Status = paymentdomain.PaymentClosed
	case "REFUND":
		state.Status = paymentdomain.PaymentRefunded
	default:
		state.Status = paymentdomain.PaymentPending
	}
	return state, nil
}

func (w *Wechat) ParseNotify(_ context.Context, headers map[string]string, body []byte) (*paymentdomain.Notification, error) {
	if err := w.requireConfigured(); err != nil {
		return nil, err
	}
	timestamp := headers["wechatpay-timestamp"]
	nonce := headers["wechatpay-nonce"]
	signature := headers["wechatpay-signature"]
	if timestamp == "" || nonce == "" || signature == "" {
		return nil, errors.New("wechat notify is missing signature headers")
	}
	message := timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	if err := VerifySHA256(w.config.PlatformPublicKey, message, signature); err != nil {
		return nil, fmt.Errorf("wechat notify signature: %w", err)
	}
	var envelope struct {
		EventType string `json:"event_type"`
		Resource  struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			Nonce          string `json:"nonce"`
			AssociatedData string `json:"associated_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("wechat notify body: %w", err)
	}
	if envelope.Resource.Algorithm != "AEAD_AES_256_GCM" {
		return nil, fmt.Errorf("wechat notify uses unsupported algorithm %q", envelope.Resource.Algorithm)
	}
	plaintext, err := DecryptResource([]byte(w.config.APIv3Key), envelope.Resource.Nonce, envelope.Resource.AssociatedData, envelope.Resource.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("wechat notify decrypt: %w", err)
	}
	var resource map[string]json.RawMessage
	if err := json.Unmarshal(plaintext, &resource); err != nil {
		return nil, fmt.Errorf("wechat notify plaintext: %w", err)
	}
	if mchid := rawString(resource["mchid"]); mchid != "" && mchid != w.config.MchID {
		return nil, fmt.Errorf("wechat notify mchid %q does not match the configured merchant", mchid)
	}
	if appid := rawString(resource["appid"]); appid != "" && appid != w.config.AppID {
		return nil, fmt.Errorf("wechat notify appid %q does not match the configured app", appid)
	}
	notification := &paymentdomain.Notification{Raw: body}
	if billNo := rawString(resource["out_bill_no"]); billNo != "" {
		notification.Kind = paymentdomain.NotifyKindPayout
		notification.BillNo = billNo
		notification.TradeID = rawString(resource["transfer_bill_no"])
		switch rawString(resource["state"]) {
		case "SUCCESS":
			notification.Status = paymentdomain.SettlementPaid
		case "FAIL", "CANCELLED", "CANCELING":
			notification.Status = paymentdomain.SettlementFailed
		default:
			notification.Status = paymentdomain.SettlementProcessing
		}
		return notification, nil
	}
	if refundNo := rawString(resource["out_refund_no"]); refundNo != "" {
		notification.Kind = paymentdomain.NotifyKindRefund
		notification.RefundNo = refundNo
		notification.OrderNo = rawString(resource["out_trade_no"])
		notification.TradeID = rawString(resource["transaction_id"])
		var amount struct {
			Refund int64 `json:"refund"`
		}
		if raw, ok := resource["amount"]; ok {
			if err := json.Unmarshal(raw, &amount); err != nil {
				return nil, fmt.Errorf("wechat refund notify amount: %w", err)
			}
		}
		notification.AmountFen = amount.Refund
		switch rawString(resource["refund_status"]) {
		case "SUCCESS":
			notification.Status = paymentdomain.PaymentRefunded
		case "CLOSED", "ABNORMAL":
			notification.Status = paymentdomain.PaymentRefundFailed
		default:
			notification.Status = paymentdomain.PaymentRefundPending
		}
		if notification.OrderNo == "" {
			return nil, errors.New("wechat refund notify is missing out_trade_no")
		}
		return notification, nil
	}
	notification.Kind = paymentdomain.NotifyKindCharge
	notification.OrderNo = rawString(resource["out_trade_no"])
	notification.TradeID = rawString(resource["transaction_id"])
	if notification.OrderNo == "" {
		return nil, errors.New("wechat notify is missing out_trade_no")
	}
	var amount struct {
		Total int64 `json:"total"`
	}
	if raw, ok := resource["amount"]; ok {
		if err := json.Unmarshal(raw, &amount); err != nil {
			return nil, fmt.Errorf("wechat notify amount: %w", err)
		}
	}
	notification.AmountFen = amount.Total
	switch rawString(resource["trade_state"]) {
	case "SUCCESS":
		notification.Status = paymentdomain.PaymentPaid
		if paidAt := rawString(resource["success_time"]); paidAt != "" {
			if parsed, err := time.Parse(time.RFC3339, paidAt); err == nil {
				utc := parsed.UTC()
				notification.PaidAt = &utc
			}
		}
	case "CLOSED":
		notification.Status = paymentdomain.PaymentClosed
	default:
		notification.Status = paymentdomain.PaymentPending
	}
	return notification, nil
}

func (w *Wechat) Transfer(ctx context.Context, request paymentdomain.PayoutRequest) (*paymentdomain.Payout, error) {
	if err := w.requireConfigured(); err != nil {
		return nil, err
	}
	openID := strings.TrimSpace(request.Account.Account)
	if openID == "" {
		return nil, errors.New("rider openid is empty")
	}
	payload := map[string]any{
		"appid":             w.config.AppID,
		"out_bill_no":       request.BillNo,
		"transfer_scene_id": w.config.TransferSceneID,
		"openid":            openID,
		"transfer_amount":   request.AmountFen,
		"transfer_remark":   truncate(request.Remark, 32),
		"notify_url":        w.config.NotifyURL,
	}
	var result struct {
		OutBillNo      string `json:"out_bill_no"`
		TransferBillNo string `json:"transfer_bill_no"`
		State          string `json:"state"`
		FailReason     string `json:"fail_reason"`
	}
	if err := w.do(ctx, http.MethodPost, "/v3/fund-app/mch-transfer/transfer-bills", payload, &result); err != nil {
		return nil, err
	}
	payout := &paymentdomain.Payout{
		BillNo: firstNonEmpty(result.TransferBillNo, result.OutBillNo, request.BillNo),
		Status: paymentdomain.SettlementProcessing,
	}
	switch result.State {
	case "SUCCESS":
		payout.Status = paymentdomain.SettlementPaid
	case "FAIL", "CANCELLED", "CANCELING":
		payout.Status = paymentdomain.SettlementFailed
		payout.FailReason = firstNonEmpty(result.FailReason, "wechat transfer failed")
	}
	return payout, nil
}

func (w *Wechat) Refund(ctx context.Context, request paymentdomain.RefundRequest) (*paymentdomain.Refund, error) {
	if err := w.requireConfigured(); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"out_trade_no":  request.OrderNo,
		"out_refund_no": request.RefundNo,
		"reason":        truncate(request.Reason, 80),
		"notify_url":    w.config.NotifyURL,
		"amount": map[string]any{
			"refund": request.AmountFen, "total": request.AmountFen, "currency": paymentdomain.CurrencyCNY,
		},
	}
	var result struct {
		RefundID string `json:"refund_id"`
		Status   string `json:"status"`
	}
	if err := w.do(ctx, http.MethodPost, "/v3/refund/domestic/refunds", payload, &result); err != nil {
		return nil, err
	}
	refund := &paymentdomain.Refund{RefundID: firstNonEmpty(result.RefundID, request.RefundNo)}
	switch result.Status {
	case "SUCCESS":
		refund.Status = paymentdomain.PaymentRefunded
	case "PROCESSING":
		refund.Status = paymentdomain.PaymentRefundPending
	case "CLOSED", "ABNORMAL":
		refund.Status = paymentdomain.PaymentRefundFailed
		refund.FailReason = "wechat refund ended in state " + result.Status
	default:
		return nil, fmt.Errorf("wechat refund ended in state %q", result.Status)
	}
	return refund, nil
}

func (w *Wechat) Close(ctx context.Context, orderNo string) error {
	if err := w.requireConfigured(); err != nil {
		return err
	}
	path := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(orderNo) + "/close"
	err := w.do(ctx, http.MethodPost, path, map[string]any{"mchid": w.config.MchID}, nil)
	if err != nil && (strings.Contains(err.Error(), "ORDER_NOT_EXIST") || strings.Contains(err.Error(), "ORDER_CLOSED")) {
		return nil
	}
	return err
}

func (w *Wechat) QueryRefund(ctx context.Context, _, refundNo string) (*paymentdomain.Refund, error) {
	if err := w.requireConfigured(); err != nil {
		return nil, err
	}
	var result struct {
		RefundID string `json:"refund_id"`
		Status   string `json:"status"`
	}
	if err := w.do(ctx, http.MethodGet, "/v3/refund/domestic/refunds/"+url.PathEscape(refundNo), nil, &result); err != nil {
		return nil, err
	}
	refund := &paymentdomain.Refund{RefundID: firstNonEmpty(result.RefundID, refundNo)}
	switch result.Status {
	case "SUCCESS":
		refund.Status = paymentdomain.PaymentRefunded
	case "PROCESSING":
		refund.Status = paymentdomain.PaymentRefundPending
	case "CLOSED", "ABNORMAL":
		refund.Status = paymentdomain.PaymentRefundFailed
		refund.FailReason = "wechat refund ended in state " + result.Status
	default:
		refund.Status = paymentdomain.PaymentRefundPending
	}
	return refund, nil
}

func (w *Wechat) QueryPayout(ctx context.Context, billNo string) (*paymentdomain.Payout, error) {
	if err := w.requireConfigured(); err != nil {
		return nil, err
	}
	var result struct {
		OutBillNo      string `json:"out_bill_no"`
		TransferBillNo string `json:"transfer_bill_no"`
		State          string `json:"state"`
		FailReason     string `json:"fail_reason"`
	}
	if err := w.do(ctx, http.MethodGet, "/v3/fund-app/mch-transfer/transfer-bills/out-bill-no/"+url.PathEscape(billNo), nil, &result); err != nil {
		return nil, err
	}
	payout := &paymentdomain.Payout{
		BillNo: firstNonEmpty(result.TransferBillNo, result.OutBillNo, billNo),
		Status: paymentdomain.SettlementProcessing,
	}
	switch result.State {
	case "SUCCESS":
		payout.Status = paymentdomain.SettlementPaid
	case "FAIL", "CANCELLED", "CANCELING":
		payout.Status = paymentdomain.SettlementFailed
		payout.FailReason = firstNonEmpty(result.FailReason, "wechat transfer failed")
	}
	return payout, nil
}

func (w *Wechat) requireConfigured() error {
	if !w.Configured() {
		return fmt.Errorf("%w: set WECHAT_PAY_MCH_ID, WECHAT_PAY_APP_ID, WECHAT_PAY_SERIAL_NO, WECHAT_PAY_API_V3_KEY, the key files and WECHAT_PAY_NOTIFY_URL", paymentdomain.ErrNotConfigured)
	}
	return nil
}

func (w *Wechat) do(ctx context.Context, method, path string, payload any, out any) error {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	authorization, err := w.authorization(method, path, string(body))
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, w.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", authorization)
	response, err := w.config.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("wechat %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("wechat %s %s: http %d: %s", method, path, response.StatusCode, truncate(string(raw), 300))
	}
	// API v3 responses carry the same signature headers as notifications.
	// Verify them when present, before trusting any field.
	if signature := response.Header.Get("Wechatpay-Signature"); signature != "" {
		message := response.Header.Get("Wechatpay-Timestamp") + "\n" +
			response.Header.Get("Wechatpay-Nonce") + "\n" + string(raw) + "\n"
		if err := VerifySHA256(w.config.PlatformPublicKey, message, signature); err != nil {
			return fmt.Errorf("wechat %s %s: response signature: %w", method, path, err)
		}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("wechat %s %s: invalid response: %w", method, path, err)
		}
	}
	return nil
}

// authorization builds the WECHATPAY2-SHA256-RSA2048 header. The signed message
// includes the exact path and query string that is sent on the wire.
func (w *Wechat) authorization(method, path, body string) (string, error) {
	nonce, err := randomHex(16)
	if err != nil {
		return "", err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	message := method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + body + "\n"
	signature, err := SignSHA256(w.config.PrivateKey, message)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		w.config.MchID, nonce, signature, timestamp, w.config.SerialNumber), nil
}

// DecryptResource opens the AEAD_AES_256_GCM payload of an API v3 notification.
func DecryptResource(key []byte, nonce, associatedData, ciphertext string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceBytes := []byte(nonce)
	if len(nonceBytes) != gcm.NonceSize() {
		decoded, err := base64.StdEncoding.DecodeString(nonce)
		if err != nil || len(decoded) != gcm.NonceSize() {
			return nil, fmt.Errorf("nonce has length %d, want %d", len(nonceBytes), gcm.NonceSize())
		}
		nonceBytes = decoded
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("ciphertext is not base64: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonceBytes, data, []byte(associatedData))
	if err != nil {
		return nil, fmt.Errorf("decrypt payload: %w", err)
	}
	return plaintext, nil
}

func randomHex(length int) (string, error) {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

var _ paymentdomain.Gateway = (*Wechat)(nil)
var _ paymentdomain.Closer = (*Wechat)(nil)
var _ paymentdomain.RefundQuerier = (*Wechat)(nil)
var _ paymentdomain.PayoutQuerier = (*Wechat)(nil)
