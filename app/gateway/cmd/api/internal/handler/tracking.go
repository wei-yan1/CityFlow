package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"cityflow/app/gateway/cmd/api/internal/svc"
	trackingclient "cityflow/app/tracking/cmd/rpc/trackingclient"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

func ReportLocation(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Tracking == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "tracking service unavailable")
			return
		}
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Tracking.Report(rpcContext(r), &trackingclient.JsonBody{BodyJson: body})
		if err != nil {
			writeTrackingError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func TaskLocation(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Tracking == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "tracking service unavailable")
			return
		}
		reply, err := s.Tracking.Snapshot(rpcContext(r), &trackingclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTrackingError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func TaskLocationHistory(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Tracking == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "tracking service unavailable")
			return
		}
		after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		limit, limitErr := strconv.Atoi(r.URL.Query().Get("limit"))
		if r.URL.Query().Get("after") == "" {
			after = 0
			err = nil
		}
		if r.URL.Query().Get("limit") == "" {
			limit = 100
			limitErr = nil
		}
		if err != nil || limitErr != nil || limit < 1 || limit > 500 {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid history cursor or limit (1-500)")
			return
		}
		id := pathvar.Vars(r)["id"]
		// Fetch one extra row so the gateway can return has_more without a
		// second query. Tracking treats `after` as exclusive, so no cursor
		// adjustment or duplicate stripping is needed here.
		fetchAfter, fetchLimit := after, limit+1
		body, _ := json.Marshal(map[string]any{"after": fetchAfter, "limit": fetchLimit})
		reply, err := s.Tracking.History(rpcContext(r), &trackingclient.IdBody{Id: id, BodyJson: body})
		if err != nil {
			writeTrackingError(w, err)
			return
		}
		var points []map[string]any
		if err := json.Unmarshal(reply.GetBodyJson(), &points); err != nil {
			writeTrackingError(w, err)
			return
		}
		more := len(points) > limit
		if more {
			points = points[:limit]
		}
		next := after
		if len(points) > 0 {
			if last, ok := points[len(points)-1]["id"].(float64); ok {
				next = uint64(last)
			}
		}
		out, _ := json.Marshal(map[string]any{
			"data":              points,
			"coordinate_system": "WGS84",
			"next_cursor":       next,
			"has_more":          more,
		})
		writeRawJSON(w, http.StatusOK, out)
	}
}

func NearbyRiders(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Tracking == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "tracking service unavailable")
			return
		}
		latitude, _ := strconv.ParseFloat(r.URL.Query().Get("latitude"), 64)
		longitude, _ := strconv.ParseFloat(r.URL.Query().Get("longitude"), 64)
		radius, _ := strconv.ParseFloat(r.URL.Query().Get("radius_meters"), 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		query, _ := json.Marshal(map[string]any{
			"latitude":      latitude,
			"longitude":     longitude,
			"radius_meters": radius,
			"limit":         limit,
		})
		reply, err := s.Tracking.Nearby(rpcContext(r), &trackingclient.QueryRequest{QueryJson: query})
		if err != nil {
			writeTrackingError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func writeTrackingError(w http.ResponseWriter, err error) {
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "authentication required",
		Forbidden:       "operation is not allowed",
		NotFound:        "resource not found",
		Conflict:        "operation conflict",
		BadRequest:      "invalid request",
		TooManyRequests: "location reports are too frequent; retry later",
		Unavailable:     "tracking service unavailable",
		Internal:        "could not process tracking request",
	})
}
