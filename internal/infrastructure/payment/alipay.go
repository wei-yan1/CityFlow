package payment

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	paymentdomain "cityflow/internal/domain/payment"
)

const alipayDefaultGateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"

// Alipay holds the merchant credentials. Every field is optional at
// construction time so the application still boots before the user fills them
// in; requests then fail with ErrNotConfigured instead of pretending to pay.
type AlipayConfig struct {
	AppID      string
	GatewayURL string
	NotifyURL  string
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
	HTTPClient *http.Client
}

type Alipay struct{ config AlipayConfig }

func NewAlipay(config AlipayConfig) *Alipay {
	if strings.TrimSpace(config.GatewayURL) == "" {
		config.GatewayURL = alipayDefaultGateway
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Alipay{config: config}
}

func (a *Alipay) Name() string { return paymentdomain.ProviderAlipay }

func (a *Alipay) Configured() bool {
	return strings.TrimSpace(a.config.AppID) != "" && a.config.PrivateKey != nil &&
		a.config.PublicKey != nil && strings.TrimSpace(a.config.NotifyURL) != ""
}

func (a *Alipay) Precreate(ctx context.Context, request paymentdomain.ChargeRequest) (*paymentdomain.Charge, error) {
	if err := a.requireConfigured(); err != nil {
		return nil, err
	}
	minutes := int(time.Until(request.ExpiresAt).Minutes())
	if minutes < 1 {
		minutes = 1
	}
	biz := map[string]any{
		"out_trade_no":    request.OrderNo,
		"total_amount":    FenToYuan(request.AmountFen),
		"subject":         truncate(request.Subject, 256),
		"timeout_express": fmt.Sprintf("%dm", minutes),
	}
	node, err := a.call(ctx, "alipay.trade.precreate", biz)
	if err != nil {
		return nil, err
	}
	code := rawString(node["qr_code"])
	if code == "" {
		return nil, errors.New("alipay precreate returned an empty qr_code")
	}
	return &paymentdomain.Charge{
		Provider: paymentdomain.ProviderAlipay, OrderNo: request.OrderNo,
		QRCode: code, PayURL: code, ExpiresAt: request.ExpiresAt,
	}, nil
}

func (a *Alipay) Query(ctx context.Context, orderNo string) (*paymentdomain.ChargeState, error) {
	if err := a.requireConfigured(); err != nil {
		return nil, err
	}
	node, err := a.call(ctx, "alipay.trade.query", map[string]any{"out_trade_no": orderNo})
	if err != nil {
		return nil, err
	}
	amount, err := YuanToFen(rawString(node["total_amount"]))
	if err != nil {
		return nil, fmt.Errorf("alipay query amount: %w", err)
	}
	state := &paymentdomain.ChargeState{
		OrderNo: rawString(node["out_trade_no"]), TradeID: rawString(node["trade_no"]), AmountFen: amount,
	}
	switch rawString(node["trade_status"]) {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		state.Status = paymentdomain.PaymentPaid
		if paidAt, ok := parseShanghaiTime(rawString(node["gmt_payment"])); ok {
			state.PaidAt = &paidAt
		}
	case "TRADE_CLOSED":
		state.Status = paymentdomain.PaymentClosed
	default:
		state.Status = paymentdomain.PaymentPending
	}
	if state.OrderNo == "" {
		state.OrderNo = orderNo
	}
	return state, nil
}

func (a *Alipay) ParseNotify(_ context.Context, _ map[string]string, body []byte) (*paymentdomain.Notification, error) {
	if err := a.requireConfigured(); err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("alipay notify form: %w", err)
	}
	signature := values.Get("sign")
	if signature == "" {
		return nil, errors.New("alipay notify is missing a signature")
	}
	values.Del("sign")
	values.Del("sign_type")
	if err := VerifySHA256(a.config.PublicKey, alipayCanonical(values), signature); err != nil {
		return nil, fmt.Errorf("alipay notify signature: %w", err)
	}
	if appID := values.Get("app_id"); appID != "" && appID != a.config.AppID {
		return nil, fmt.Errorf("alipay notify app_id %q does not match the configured app", appID)
	}
	amount, err := YuanToFen(values.Get("total_amount"))
	if err != nil {
		return nil, fmt.Errorf("alipay notify amount: %w", err)
	}
	notification := &paymentdomain.Notification{
		Kind: paymentdomain.NotifyKindCharge, OrderNo: values.Get("out_trade_no"),
		TradeID: values.Get("trade_no"), AmountFen: amount, Raw: body,
	}
	switch values.Get("trade_status") {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		notification.Status = paymentdomain.PaymentPaid
		if paidAt, ok := parseShanghaiTime(values.Get("gmt_payment")); ok {
			notification.PaidAt = &paidAt
		}
	case "TRADE_CLOSED":
		notification.Status = paymentdomain.PaymentClosed
	default:
		notification.Status = paymentdomain.PaymentPending
	}
	if notification.OrderNo == "" {
		return nil, errors.New("alipay notify is missing out_trade_no")
	}
	return notification, nil
}

func (a *Alipay) Transfer(ctx context.Context, request paymentdomain.PayoutRequest) (*paymentdomain.Payout, error) {
	if err := a.requireConfigured(); err != nil {
		return nil, err
	}
	account := strings.TrimSpace(request.Account.Account)
	if account == "" {
		return nil, errors.New("rider payout account is empty")
	}
	payee := map[string]any{
		"identity":      account,
		"identity_type": alipayIdentityType(account),
	}
	if name := strings.TrimSpace(request.Account.AccountName); name != "" {
		payee["name"] = name
	}
	biz := map[string]any{
		"out_biz_no":   request.BillNo,
		"trans_amount": FenToYuan(request.AmountFen),
		"product_code": "TRANS_ACCOUNT_NO_PWD",
		"biz_scene":    "DIRECT_TRANSFER",
		"order_title":  truncate(request.Remark, 64),
		"remark":       truncate(request.Remark, 200),
		"payee_info":   payee,
	}
	node, err := a.call(ctx, "alipay.fund.trans.uni.transfer", biz)
	if err != nil {
		return nil, err
	}
	payout := &paymentdomain.Payout{
		BillNo: firstNonEmpty(rawString(node["pay_fund_order_id"]), rawString(node["order_id"]), request.BillNo),
		Status: paymentdomain.SettlementPaid,
	}
	switch rawString(node["status"]) {
	case "SUCCESS":
	case "DEALING":
		payout.Status = paymentdomain.SettlementProcessing
	default:
		payout.Status = paymentdomain.SettlementFailed
		payout.FailReason = firstNonEmpty(rawString(node["sub_msg"]), rawString(node["msg"]), "alipay transfer did not succeed")
	}
	return payout, nil
}

func (a *Alipay) Refund(ctx context.Context, request paymentdomain.RefundRequest) (*paymentdomain.Refund, error) {
	if err := a.requireConfigured(); err != nil {
		return nil, err
	}
	biz := map[string]any{
		"out_trade_no":   request.OrderNo,
		"refund_amount":  FenToYuan(request.AmountFen),
		"refund_reason":  truncate(request.Reason, 256),
		"out_request_no": request.RefundNo,
	}
	if _, err := a.call(ctx, "alipay.trade.refund", biz); err != nil {
		return nil, err
	}
	return &paymentdomain.Refund{RefundID: request.RefundNo, Status: paymentdomain.PaymentRefunded}, nil
}

// Close cancels the provider-side trade. It is called before a local expiry
// close so a QR code that is no longer usable locally cannot still be paid.
func (a *Alipay) Close(ctx context.Context, orderNo string) error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	_, err := a.call(ctx, "alipay.trade.close", map[string]any{"out_trade_no": orderNo})
	if err != nil && (strings.Contains(err.Error(), "TRADE_NOT_EXIST") || strings.Contains(err.Error(), "ACQ.TRADE_NOT_EXIST")) {
		return nil
	}
	return err
}

func (a *Alipay) QueryRefund(ctx context.Context, orderNo, refundNo string) (*paymentdomain.Refund, error) {
	if err := a.requireConfigured(); err != nil {
		return nil, err
	}
	node, err := a.call(ctx, "alipay.trade.fastpay.refund.query", map[string]any{
		"out_trade_no": orderNo, "out_request_no": refundNo,
	})
	if err != nil {
		return nil, err
	}
	refund := &paymentdomain.Refund{RefundID: firstNonEmpty(rawString(node["refund_id"]), refundNo)}
	if rawString(node["refund_status"]) == "REFUND_SUCCESS" {
		refund.Status = paymentdomain.PaymentRefunded
	} else {
		refund.Status = paymentdomain.PaymentRefundPending
	}
	return refund, nil
}

func (a *Alipay) QueryPayout(ctx context.Context, billNo string) (*paymentdomain.Payout, error) {
	if err := a.requireConfigured(); err != nil {
		return nil, err
	}
	node, err := a.call(ctx, "alipay.fund.trans.common.query", map[string]any{
		"out_biz_no": billNo, "product_code": "TRANS_ACCOUNT_NO_PWD", "biz_scene": "DIRECT_TRANSFER",
	})
	if err != nil {
		return nil, err
	}
	payout := &paymentdomain.Payout{BillNo: firstNonEmpty(rawString(node["order_id"]), billNo)}
	switch rawString(node["status"]) {
	case "SUCCESS":
		payout.Status = paymentdomain.SettlementPaid
	case "DEALING":
		payout.Status = paymentdomain.SettlementProcessing
	case "FAIL":
		payout.Status = paymentdomain.SettlementFailed
		payout.FailReason = firstNonEmpty(rawString(node["fail_reason"]), rawString(node["sub_msg"]), "alipay transfer failed")
	default:
		payout.Status = paymentdomain.SettlementProcessing
	}
	return payout, nil
}

func (a *Alipay) requireConfigured() error {
	if !a.Configured() {
		return fmt.Errorf("%w: set ALIPAY_APP_ID, the RSA keys and ALIPAY_NOTIFY_URL", paymentdomain.ErrNotConfigured)
	}
	return nil
}

// call signs the request, posts it as a form and verifies the response
// signature. The raw response node bytes are what Alipay signs, so they are
// verified before any field is trusted.
func (a *Alipay) call(ctx context.Context, method string, biz any) (map[string]json.RawMessage, error) {
	content, err := json.Marshal(biz)
	if err != nil {
		return nil, err
	}
	params := map[string]string{
		"app_id":      a.config.AppID,
		"method":      method,
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().In(shanghaiZone).Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": string(content),
		"notify_url":  a.config.NotifyURL,
	}
	signature, err := SignSHA256(a.config.PrivateKey, alipayCanonicalText(params))
	if err != nil {
		return nil, err
	}
	params["sign"] = signature
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.GatewayURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	response, err := a.config.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("alipay %s: %w", method, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("alipay %s: http %d: %s", method, response.StatusCode, truncate(string(raw), 300))
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("alipay %s: invalid JSON: %s", method, truncate(string(raw), 300))
	}
	nodeRaw, ok := envelope[strings.ReplaceAll(method, ".", "_")+"_response"]
	if !ok {
		nodeRaw, ok = envelope["error_response"]
	}
	if !ok {
		return nil, fmt.Errorf("alipay %s: unexpected response: %s", method, truncate(string(raw), 300))
	}
	if signRaw, present := envelope["sign"]; present {
		var signature string
		if err := json.Unmarshal(signRaw, &signature); err != nil {
			return nil, fmt.Errorf("alipay %s: response signature: %w", method, err)
		}
		if err := VerifySHA256(a.config.PublicKey, string(nodeRaw), signature); err != nil {
			return nil, fmt.Errorf("alipay %s: response signature: %w", method, err)
		}
	} else {
		return nil, fmt.Errorf("alipay %s: response is missing a signature", method)
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(nodeRaw, &node); err != nil {
		return nil, fmt.Errorf("alipay %s: invalid node: %w", method, err)
	}
	if code := rawString(node["code"]); code != "" && code != "10000" {
		return nil, fmt.Errorf("alipay %s failed: %s %s %s", method, code,
			firstNonEmpty(rawString(node["msg"]), "error"), firstNonEmpty(rawString(node["sub_msg"]), ""))
	}
	return node, nil
}

// alipayCanonical sorts decoded form values for notification verification.
func alipayCanonical(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key, list := range values {
		if len(list) == 0 || strings.TrimSpace(list[0]) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(values.Get(key))
	}
	return builder.String()
}

func alipayCanonicalText(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(params[key])
	}
	return builder.String()
}

func alipayIdentityType(account string) string {
	if len(account) == 16 && strings.HasPrefix(account, "2088") {
		return "ALIPAY_USER_ID"
	}
	return "ALIPAY_LOGON_ID"
}

var shanghaiZone = time.FixedZone("CST", 8*60*60)

func parseShanghaiTime(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, shanghaiZone)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

var _ paymentdomain.Gateway = (*Alipay)(nil)
var _ paymentdomain.Closer = (*Alipay)(nil)
var _ paymentdomain.RefundQuerier = (*Alipay)(nil)
var _ paymentdomain.PayoutQuerier = (*Alipay)(nil)
