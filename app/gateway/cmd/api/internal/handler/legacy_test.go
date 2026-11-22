package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cityflow/app/gateway/cmd/api/internal/svc"
)

func TestPagesServeEmbeddedWebTree(t *testing.T) {
	next := Pages(&svc.ServiceContext{})
	recorder := httptest.NewRecorder()
	next(recorder, httptest.NewRequest(http.MethodGet, "/user", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "CityFlow") {
		t.Fatalf("page body is not the embedded user page")
	}
	if recorder.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("content-type=%q", recorder.Header().Get("Content-Type"))
	}
}

func TestPagesServeNestedAssets(t *testing.T) {
	next := Pages(&svc.ServiceContext{})
	recorder := httptest.NewRecorder()
	next(recorder, httptest.NewRequest(http.MethodGet, "/assets/shared/common.js", nil))

	if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
		t.Fatalf("status=%d bytes=%d", recorder.Code, recorder.Body.Len())
	}
}

func TestPagesServeRedeemCodeAdminPage(t *testing.T) {
	next := Pages(&svc.ServiceContext{})
	recorder := httptest.NewRecorder()
	next(recorder, httptest.NewRequest(http.MethodGet, "/admin/redeem-codes", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "兑换码管理") || !strings.Contains(body, "redeemForm") {
		t.Fatalf("page body is not the redeem-code admin page")
	}
}

func TestPagesServeRedeemCodeAdminAsset(t *testing.T) {
	next := Pages(&svc.ServiceContext{})
	recorder := httptest.NewRecorder()
	next(recorder, httptest.NewRequest(http.MethodGet, "/assets/platform/redeem-codes.js", nil))

	if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
		t.Fatalf("status=%d bytes=%d", recorder.Code, recorder.Body.Len())
	}
}

func TestPagesRejectUnknownAPIRoutes(t *testing.T) {
	next := Pages(&svc.ServiceContext{})
	recorder := httptest.NewRecorder()
	next(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/unmigrated", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
