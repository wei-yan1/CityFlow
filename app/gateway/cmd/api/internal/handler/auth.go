package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/logic"
	"cityflow/app/gateway/cmd/api/internal/svc"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/platform/authctx"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/httpkit"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/rest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const refreshCookieName = "cityflow_refresh"

func setRefreshCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     "/api/v1/auth",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		strings.EqualFold(parsed.Host, r.Host)
}

// Login is the public password exchange. Identity owns credentials and refresh
// sessions; Gateway only sets the HttpOnly cookie and returns the access token.
func Login(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request auth.LoginRequest
		if err := httpkit.DecodeJSON(w, r, &request); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(request.Email) == "" || request.Password == "" {
			httpkit.WriteError(w, http.StatusBadRequest, "email and password are required")
			return
		}
		response, err := s.Identity.Login(r.Context(), &identityclient.LoginRequest{
			Email: strings.TrimSpace(request.Email), Password: request.Password,
		})
		if err != nil {
			writeAuthError(w, err, "could not authenticate account")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if response.GetRefreshToken() != "" {
			setRefreshCookie(w, r, response.GetRefreshToken(), 7*24*3600)
		}
		httpkit.WriteJSON(w, http.StatusOK, logic.AuthResponse(response))
	}
}

func Refresh(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			httpkit.WriteError(w, http.StatusForbidden, "请求来源无效")
			return
		}
		cookie, err := r.Cookie(refreshCookieName)
		if err != nil || cookie.Value == "" {
			httpkit.WriteError(w, http.StatusUnauthorized, "请重新登录")
			return
		}
		response, err := s.Identity.Refresh(r.Context(), &identityclient.RefreshRequest{RefreshToken: cookie.Value})
		if err != nil {
			if status.Code(err) == codes.Unauthenticated {
				setRefreshCookie(w, r, "", -1)
				httpkit.WriteError(w, http.StatusUnauthorized, "登录已过期，请重新登录")
				return
			}
			writeAuthError(w, err, "登录服务暂不可用，请稍后重试")
			return
		}
		if response.GetRefreshToken() != "" {
			setRefreshCookie(w, r, response.GetRefreshToken(), 7*24*3600)
		}
		w.Header().Set("Cache-Control", "no-store")
		httpkit.WriteJSON(w, http.StatusOK, logic.AuthResponse(response))
	}
}

func Logout(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			httpkit.WriteError(w, http.StatusForbidden, "请求来源无效")
			return
		}
		token := ""
		if cookie, err := r.Cookie(refreshCookieName); err == nil {
			token = cookie.Value
		}
		if _, err := s.Identity.Logout(ctxWithToken(r, token), &identityclient.LogoutRequest{RefreshToken: token}); err != nil {
			writeAuthError(w, err, "退出失败，请重试")
			return
		}
		setRefreshCookie(w, r, "", -1)
		w.WriteHeader(http.StatusNoContent)
	}
}

func Me(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := authn.BearerToken(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		response, err := s.Identity.Me(rpcctx.WithBearer(r.Context(), token), &identityclient.MeRequest{})
		if err != nil {
			writeAuthError(w, err, "could not load user")
			return
		}
		httpkit.WriteJSON(w, http.StatusOK, logic.User(response.GetUser()))
	}
}

// Auth verifies the bearer token locally at the edge, then consults a short
// TTL account-status cache. Identity is called only on a cache miss; this keeps
// a disabled account from being a per-request serial dependency while Task
// still re-verifies the same token for its own resource decisions.
func Auth(s *svc.ServiceContext) rest.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token, ok := authn.BearerToken(r)
			if !ok {
				httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if s.Status == nil {
				httpkit.WriteError(w, http.StatusServiceUnavailable, "authentication status unavailable")
				return
			}
			claims, err := authn.FromToken(r.Context(), token, s.Secret, s.Status.IsActive)
			if err != nil {
				writeAuthnError(w, err)
				return
			}
			r.Header.Del("X-Cityflow-User-Id")
			r.Header.Del("X-Cityflow-Role")
			r.Header.Del("X-Cityflow-Authenticated")
			next(w, r.WithContext(authctx.WithClaims(r.Context(), claims)))
		}
	}
}

func writeAuthError(w http.ResponseWriter, err error, fallback string) {
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "invalid email or password",
		BadRequest:      "invalid request",
		Unavailable:     fallback,
		Internal:        fallback,
	})
}

func writeAuthnError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrUnavailable) {
		httpkit.WriteError(w, http.StatusServiceUnavailable, "authentication service unavailable")
		return
	}
	httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
}

func ctxWithToken(r *http.Request, token string) context.Context {
	if token == "" {
		return r.Context()
	}
	return rpcctx.WithBearer(r.Context(), token)
}
