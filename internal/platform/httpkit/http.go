// Package httpkit holds the small JSON/error conventions shared by the new
// go-zero services. It deliberately returns the same error body shape as the
// existing Gin API so the browser can migrate without a second response format.
package httpkit

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"

	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"
)

// DecodeJSON reads one strict JSON value with the same 1 MiB limit as the
// existing HTTP contract.
func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is required")
		}
		return errors.New("invalid JSON: " + err.Error())
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON value")
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]string{"error": message})
}

// GRPCErrorMessages lets each edge route family choose safe public wording
// while keeping the status-code mapping in one place. Empty fields fall back to
// the generic message for that class.
type GRPCErrorMessages struct {
	Unauthenticated string
	Forbidden       string
	NotFound        string
	Conflict        string
	BadRequest      string
	TooManyRequests string
	Unavailable     string
	Internal        string
}

// WriteGRPCError maps a downstream gRPC error to one public HTTP error shape.
// Raw internal error text is never exposed for Internal/Unavailable classes.
func WriteGRPCError(w http.ResponseWriter, err error, messages GRPCErrorMessages) {
	code := grpcStatus.Code(err)
	switch code {
	case codes.Unauthenticated:
		WriteError(w, http.StatusUnauthorized, fallbackMessage(messages.Unauthenticated, "authentication required"))
	case codes.PermissionDenied:
		WriteError(w, http.StatusForbidden, fallbackMessage(messages.Forbidden, "operation is not allowed"))
	case codes.NotFound:
		WriteError(w, http.StatusNotFound, fallbackMessage(messages.NotFound, "resource not found"))
	case codes.InvalidArgument:
		WriteError(w, http.StatusBadRequest, fallbackMessage(grpcStatus.Convert(err).Message(), fallbackMessage(messages.BadRequest, "invalid request")))
	case codes.FailedPrecondition, codes.AlreadyExists, codes.Aborted:
		WriteError(w, http.StatusConflict, fallbackMessage(grpcStatus.Convert(err).Message(), fallbackMessage(messages.Conflict, "operation conflict")))
	case codes.ResourceExhausted:
		w.Header().Set("Retry-After", "1")
		WriteError(w, http.StatusTooManyRequests, fallbackMessage(messages.TooManyRequests, "service is busy"))
	case codes.DeadlineExceeded:
		WriteError(w, http.StatusGatewayTimeout, fallbackMessage(messages.Unavailable, "upstream service timeout"))
	case codes.Unavailable:
		WriteError(w, http.StatusServiceUnavailable, fallbackMessage(messages.Unavailable, "service unavailable"))
	default:
		WriteError(w, http.StatusInternalServerError, fallbackMessage(messages.Internal, "internal server error"))
	}
}

func fallbackMessage(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func Page(r *http.Request) (repository.Page, error) {
	page := repository.Page{Limit: 50}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return page, errors.New("limit must be between 1 and 200")
		}
		page.Limit = value
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 1000000 {
			return page, errors.New("offset must be between 0 and 1000000")
		}
		page.Offset = value
	}
	return page, nil
}

func WriteDomainError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		WriteError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, domain.ErrForbidden):
		WriteError(w, http.StatusForbidden, "operation is not allowed")
	case errors.Is(err, domain.ErrUnauthorized):
		WriteError(w, http.StatusUnauthorized, "account is inactive or no longer available")
	case errors.Is(err, domain.ErrConflict):
		WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrInvalidRequest):
		WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrUnavailable):
		WriteError(w, http.StatusServiceUnavailable, err.Error())
	default:
		WriteError(w, http.StatusInternalServerError, fallback)
	}
}
