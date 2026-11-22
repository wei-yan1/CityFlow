package settings

import (
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	paymentdomain "cityflow/app/payment/domain/payment"
	paymentinfra "cityflow/app/payment/internal/adapter/gateway"
)

// PaymentConfig is internal startup configuration. Keys are never serialized
// to clients and never logged.
type PaymentConfig struct {
	Provider       string
	MockBaseURL    string
	FeeBPS         int64
	Expiry         time.Duration
	SettleInterval time.Duration
	AllowMockPay   bool
	Alipay         paymentinfra.AlipayConfig
	Wechat         paymentinfra.WechatConfig
}

func Load(config PaymentConfig) (PaymentConfig, error) {
	defaults := PaymentConfig{
		Provider: paymentdomain.ProviderMock, FeeBPS: 0, Expiry: 30 * time.Minute,
		SettleInterval: 30 * time.Second, AllowMockPay: false,
		MockBaseURL: strings.TrimSpace(os.Getenv("CITYFLOW_PAYMENT_MOCK_BASE_URL")),
	}
	if config.Provider == "" {
		config.Provider = defaults.Provider
	}
	if config.Expiry <= 0 {
		config.Expiry = defaults.Expiry
	}
	if config.SettleInterval <= 0 {
		config.SettleInterval = defaults.SettleInterval
	}
	if defaults.MockBaseURL != "" {
		config.MockBaseURL = defaults.MockBaseURL
	}
	if raw := strings.ToUpper(strings.TrimSpace(os.Getenv("CITYFLOW_PAYMENT_PROVIDER"))); raw != "" {
		if !paymentdomain.ValidProvider(raw) {
			return config, fmt.Errorf("CITYFLOW_PAYMENT_PROVIDER %q is not one of MOCK, ALIPAY, WECHAT", raw)
		}
		config.Provider = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CITYFLOW_PAYMENT_FEE_BPS")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 || value > 10000 {
			return config, fmt.Errorf("CITYFLOW_PAYMENT_FEE_BPS must be an integer between 0 and 10000")
		}
		config.FeeBPS = value
	}
	if raw := strings.TrimSpace(os.Getenv("CITYFLOW_PAYMENT_EXPIRE_MINUTES")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1440 {
			return config, fmt.Errorf("CITYFLOW_PAYMENT_EXPIRE_MINUTES must be between 1 and 1440")
		}
		config.Expiry = time.Duration(value) * time.Minute
	}
	if raw := strings.TrimSpace(os.Getenv("CITYFLOW_PAYMENT_SETTLE_INTERVAL_SECONDS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 5 || value > 3600 {
			return config, fmt.Errorf("CITYFLOW_PAYMENT_SETTLE_INTERVAL_SECONDS must be between 5 and 3600")
		}
		config.SettleInterval = time.Duration(value) * time.Second
	}
	if raw := strings.TrimSpace(os.Getenv("CITYFLOW_PAYMENT_ALLOW_MOCK_PAY")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return config, fmt.Errorf("CITYFLOW_PAYMENT_ALLOW_MOCK_PAY must be true or false")
		}
		config.AllowMockPay = value
	}

	notifyBase := strings.TrimSpace(os.Getenv("CITYFLOW_PAYMENT_NOTIFY_BASE_URL"))
	alipayPrivate, err := loadSecret("ALIPAY_PRIVATE_KEY", "ALIPAY_PRIVATE_KEY_FILE")
	if err != nil {
		return config, err
	}
	alipayPublic, err := loadSecret("ALIPAY_PUBLIC_KEY", "ALIPAY_PUBLIC_KEY_FILE")
	if err != nil {
		return config, err
	}
	alipayPrivateKey, err := parseOptionalPrivateKey(alipayPrivate, "ALIPAY_PRIVATE_KEY")
	if err != nil {
		return config, err
	}
	alipayPublicKey, err := parseOptionalPublicKey(alipayPublic, "ALIPAY_PUBLIC_KEY")
	if err != nil {
		return config, err
	}
	config.Alipay = paymentinfra.AlipayConfig{
		AppID:      strings.TrimSpace(os.Getenv("ALIPAY_APP_ID")),
		GatewayURL: strings.TrimSpace(os.Getenv("ALIPAY_GATEWAY_URL")),
		NotifyURL:  resolveNotifyURL(os.Getenv("ALIPAY_NOTIFY_URL"), notifyBase, paymentdomain.ProviderAlipay),
		PrivateKey: alipayPrivateKey, PublicKey: alipayPublicKey,
	}

	wechatPrivate, err := loadSecret("WECHAT_PAY_PRIVATE_KEY", "WECHAT_PAY_PRIVATE_KEY_FILE")
	if err != nil {
		return config, err
	}
	wechatPlatform, err := loadSecret("WECHAT_PAY_PLATFORM_PUBLIC_KEY", "WECHAT_PAY_PLATFORM_PUBLIC_KEY_FILE")
	if err != nil {
		return config, err
	}
	wechatPrivateKey, err := parseOptionalPrivateKey(wechatPrivate, "WECHAT_PAY_PRIVATE_KEY")
	if err != nil {
		return config, err
	}
	wechatPlatformKey, err := parseOptionalPublicKey(wechatPlatform, "WECHAT_PAY_PLATFORM_PUBLIC_KEY")
	if err != nil {
		return config, err
	}
	config.Wechat = paymentinfra.WechatConfig{
		AppID:             strings.TrimSpace(os.Getenv("WECHAT_PAY_APP_ID")),
		MchID:             strings.TrimSpace(os.Getenv("WECHAT_PAY_MCH_ID")),
		SerialNumber:      strings.TrimSpace(os.Getenv("WECHAT_PAY_SERIAL_NO")),
		APIv3Key:          normalizeAPIv3Key(os.Getenv("WECHAT_PAY_API_V3_KEY")),
		NotifyURL:         resolveNotifyURL(os.Getenv("WECHAT_PAY_NOTIFY_URL"), notifyBase, paymentdomain.ProviderWechat),
		BaseURL:           strings.TrimSpace(os.Getenv("WECHAT_PAY_BASE_URL")),
		TransferSceneID:   strings.TrimSpace(os.Getenv("WECHAT_PAY_TRANSFER_SCENE_ID")),
		PrivateKey:        wechatPrivateKey,
		PlatformPublicKey: wechatPlatformKey,
	}
	return config, nil
}

func (c PaymentConfig) Gateways() []paymentdomain.Gateway {
	return []paymentdomain.Gateway{
		paymentinfra.NewMock(c.MockBaseURL),
		paymentinfra.NewAlipay(c.Alipay),
		paymentinfra.NewWechat(c.Wechat),
	}
}

func (c PaymentConfig) ProviderConfigured(name string) bool {
	switch name {
	case paymentdomain.ProviderAlipay:
		return paymentinfra.NewAlipay(c.Alipay).Configured()
	case paymentdomain.ProviderWechat:
		return paymentinfra.NewWechat(c.Wechat).Configured()
	default:
		return true
	}
}

// loadSecret prefers a file so PEM bodies with real newlines stay readable and
// never end up pasted into a shell history or a .env single line.
func loadSecret(inlineName, fileName string) (string, error) {
	if file := strings.TrimSpace(os.Getenv(fileName)); file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", fileName, err)
		}
		return string(content), nil
	}
	return strings.ReplaceAll(os.Getenv(inlineName), `\n`, "\n"), nil
}

func parseOptionalPrivateKey(value, name string) (*rsa.PrivateKey, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	key, err := paymentinfra.ParseRSAPrivateKey(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return key, nil
}

func parseOptionalPublicKey(value, name string) (*rsa.PublicKey, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	key, err := paymentinfra.ParseRSAPublicKey(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return key, nil
}

func resolveNotifyURL(explicit, base, provider string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/api/v1/payment-notify/" + strings.ToLower(provider)
}

func normalizeAPIv3Key(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) == 32 || trimmed == "" {
		return trimmed
	}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(decoded) == 32 {
		return string(decoded)
	}
	return trimmed
}
