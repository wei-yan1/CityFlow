package settings

import (
	"strings"
	"testing"
	"time"

	paymentdomain "cityflow/app/payment/domain/payment"
)

var paymentEnvKeys = []string{
	"CITYFLOW_PAYMENT_PROVIDER", "CITYFLOW_PAYMENT_FEE_BPS", "CITYFLOW_PAYMENT_EXPIRE_MINUTES",
	"CITYFLOW_PAYMENT_ALLOW_MOCK_PAY", "CITYFLOW_PAYMENT_SETTLE_INTERVAL_SECONDS",
	"CITYFLOW_PAYMENT_NOTIFY_BASE_URL", "CITYFLOW_PAYMENT_MOCK_BASE_URL",
	"ALIPAY_APP_ID", "ALIPAY_GATEWAY_URL", "ALIPAY_NOTIFY_URL", "ALIPAY_PRIVATE_KEY", "ALIPAY_PUBLIC_KEY",
	"ALIPAY_PRIVATE_KEY_FILE", "ALIPAY_PUBLIC_KEY_FILE",
	"WECHAT_PAY_MCH_ID", "WECHAT_PAY_APP_ID", "WECHAT_PAY_SERIAL_NO", "WECHAT_PAY_API_V3_KEY",
	"WECHAT_PAY_NOTIFY_URL", "WECHAT_PAY_BASE_URL", "WECHAT_PAY_TRANSFER_SCENE_ID",
	"WECHAT_PAY_PRIVATE_KEY", "WECHAT_PAY_PLATFORM_PUBLIC_KEY",
	"WECHAT_PAY_PRIVATE_KEY_FILE", "WECHAT_PAY_PLATFORM_PUBLIC_KEY_FILE",
}

func loadPaymentConfig() (PaymentConfig, error) {
	return Load(PaymentConfig{AllowMockPay: true})
}

func clearPaymentEnv(t *testing.T) {
	t.Helper()
	for _, key := range paymentEnvKeys {
		t.Setenv(key, "")
	}
}

func TestPaymentConfigDefaultsToMock(t *testing.T) {
	clearPaymentEnv(t)
	config, err := loadPaymentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider != paymentdomain.ProviderMock || config.FeeBPS != 0 ||
		config.Expiry != 30*time.Minute || !config.AllowMockPay ||
		config.SettleInterval != 30*time.Second {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if len(config.Gateways()) != 3 {
		t.Fatalf("all three gateways must be registered: %d", len(config.Gateways()))
	}
	if !config.ProviderConfigured(paymentdomain.ProviderMock) {
		t.Fatal("mock must always be configured")
	}
	if config.ProviderConfigured(paymentdomain.ProviderAlipay) ||
		config.ProviderConfigured(paymentdomain.ProviderWechat) {
		t.Fatal("real providers must not report configured without credentials")
	}
}

func TestPaymentConfigDerivesNotifyURLs(t *testing.T) {
	clearPaymentEnv(t)
	t.Setenv("CITYFLOW_PAYMENT_NOTIFY_BASE_URL", "https://pay.example.com/")
	config, err := loadPaymentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Alipay.NotifyURL != "https://pay.example.com/api/v1/payment-notify/alipay" ||
		config.Wechat.NotifyURL != "https://pay.example.com/api/v1/payment-notify/wechat" {
		t.Fatalf("unexpected notify urls: alipay=%q wechat=%q", config.Alipay.NotifyURL, config.Wechat.NotifyURL)
	}
}

func TestPaymentConfigRejectsInvalidValues(t *testing.T) {
	clearPaymentEnv(t)
	t.Setenv("CITYFLOW_PAYMENT_PROVIDER", "PAYPAL")
	if _, err := loadPaymentConfig(); err == nil || !strings.Contains(err.Error(), "MOCK") {
		t.Fatalf("unknown provider must fail: %v", err)
	}

	clearPaymentEnv(t)
	t.Setenv("CITYFLOW_PAYMENT_FEE_BPS", "12000")
	if _, err := loadPaymentConfig(); err == nil {
		t.Fatal("fee above 10000 must fail")
	}

	clearPaymentEnv(t)
	t.Setenv("CITYFLOW_PAYMENT_EXPIRE_MINUTES", "0")
	if _, err := loadPaymentConfig(); err == nil {
		t.Fatal("zero expiry must fail")
	}
}

func TestPaymentConfigNormalizesAPIv3Key(t *testing.T) {
	clearPaymentEnv(t)
	t.Setenv("WECHAT_PAY_API_V3_KEY", "0123456789abcdef0123456789abcdef")
	config, err := loadPaymentConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Wechat.APIv3Key) != 32 {
		t.Fatalf("api v3 key keeps 32 chars: %q", config.Wechat.APIv3Key)
	}
}
