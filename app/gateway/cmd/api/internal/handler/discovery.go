package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/app/gateway/cmd/api/internal/svc"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

func MapConfig(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Discovery == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
			return
		}
		reply, err := s.Discovery.MapConfig(r.Context(), &discoveryclient.Empty{})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func MapMerchants(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Discovery == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
			return
		}
		reply, err := s.Discovery.MapMerchants(r.Context(), &discoveryclient.Empty{})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func MapSearch(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, _ := json.Marshal(map[string]string{
			"keywords": r.URL.Query().Get("keywords"),
			"city":     r.URL.Query().Get("city"),
		})
		callMapQuery(w, r, s, "search", query)
	}
}

func MapNearby(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, _ := strconv.ParseFloat(r.URL.Query().Get("latitude"), 64)
		lon, _ := strconv.ParseFloat(r.URL.Query().Get("longitude"), 64)
		query, _ := json.Marshal(map[string]any{
			"latitude":          lat,
			"longitude":         lon,
			"coordinate_system": r.URL.Query().Get("coordinate_system"),
		})
		callMapQuery(w, r, s, "nearby", query)
	}
}

func MapRiding(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		callMapBody(w, r, s, "riding", body)
	}
}

func MapCity(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, _ := strconv.ParseFloat(r.URL.Query().Get("latitude"), 64)
		lon, _ := strconv.ParseFloat(r.URL.Query().Get("longitude"), 64)
		query, _ := json.Marshal(map[string]any{
			"latitude":          lat,
			"longitude":         lon,
			"coordinate_system": r.URL.Query().Get("coordinate_system"),
		})
		callMapQuery(w, r, s, "city", query)
	}
}

func MapReverse(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lat, _ := strconv.ParseFloat(r.URL.Query().Get("latitude"), 64)
		lon, _ := strconv.ParseFloat(r.URL.Query().Get("longitude"), 64)
		query, _ := json.Marshal(map[string]any{
			"latitude":          lat,
			"longitude":         lon,
			"coordinate_system": r.URL.Query().Get("coordinate_system"),
		})
		callMapQuery(w, r, s, "reverse", query)
	}
}

func MapDistrict(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, _ := json.Marshal(map[string]string{"adcode": r.URL.Query().Get("adcode")})
		callMapQuery(w, r, s, "district", query)
	}
}

func callMapQuery(w http.ResponseWriter, r *http.Request, s *svc.ServiceContext, kind string, query []byte) {
	if s.Discovery == nil {
		httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
		return
	}
	request := &discoveryclient.QueryRequest{QueryJson: query}
	var reply *discoveryclient.JsonReply
	var err error
	switch kind {
	case "nearby":
		reply, err = s.Discovery.MapNearby(r.Context(), request)
	case "city":
		reply, err = s.Discovery.MapCity(r.Context(), request)
	case "reverse":
		reply, err = s.Discovery.MapReverse(r.Context(), request)
	case "district":
		reply, err = s.Discovery.MapDistrict(r.Context(), request)
	default:
		reply, err = s.Discovery.MapSearch(r.Context(), request)
	}
	if err != nil {
		writeDiscoveryError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
}

func callMapBody(w http.ResponseWriter, r *http.Request, s *svc.ServiceContext, kind string, body []byte) {
	if s.Discovery == nil {
		httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
		return
	}
	request := &discoveryclient.QueryRequest{QueryJson: body}
	var reply *discoveryclient.JsonReply
	var err error
	switch kind {
	case "riding":
		reply, err = s.Discovery.MapRiding(r.Context(), request)
	default:
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		writeDiscoveryError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
}

func PlaceDetail(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Discovery == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
			return
		}
		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				httpkit.WriteError(w, http.StatusBadRequest, "invalid offset")
				return
			}
			offset = value
		}
		reply, err := s.Discovery.PlaceDetail(rpcContext(r), &discoveryclient.PlaceRequest{
			Source: pathvar.Vars(r)["source"], Id: pathvar.Vars(r)["id"], Offset: int32(offset),
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func CreatePlaceReview(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.CreatePlaceReview(rpcContext(r), &discoveryclient.PlaceRequest{
			Source: pathvar.Vars(r)["source"], Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func DeletePlaceReview(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := s.Discovery.DeleteReview(rpcContext(r), &discoveryclient.IdBody{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func MerchantMe(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply, err := s.Discovery.GetMyMerchant(rpcContext(r), &discoveryclient.IdRequest{})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func CreateMerchantMe(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.CreateMyMerchant(rpcContext(r), &discoveryclient.IdBody{BodyJson: body})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func UpdateMerchantMe(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.UpdateMyMerchant(rpcContext(r), &discoveryclient.IdBody{BodyJson: body})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func DeleteMerchantMe(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.Discovery.ArchiveMyMerchant(rpcContext(r), &discoveryclient.IdRequest{}); err != nil {
			writeDiscoveryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func MerchantMedia(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply, err := s.Discovery.GetMyMedia(rpcContext(r), &discoveryclient.IdRequest{})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func SaveMerchantMedia(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := s.Discovery.SaveMyMedia(rpcContext(r), &discoveryclient.IdBody{BodyJson: body}); err != nil {
			writeDiscoveryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func UploadMerchantImage(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "图片大小需在 5 MB 以内")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "请选择要上传的图片")
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (5<<20)+1))
		if err != nil || len(data) > 5<<20 {
			httpkit.WriteError(w, http.StatusBadRequest, "图片大小需在 5 MB 以内")
			return
		}
		reply, err := s.Discovery.UploadImage(rpcContext(r), &discoveryclient.UploadRequest{Data: data, ContentType: ""})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func ListMerchants(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := pageQuery(r)
		reply, err := s.Discovery.ListMerchants(rpcContext(r), &discoveryclient.ListQuery{
			Status: r.URL.Query().Get("status"), Limit: limit, Offset: offset,
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func ListHotMerchants(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Discovery == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
			return
		}
		limit := 10
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				limit = value
			}
		}
		body, _ := json.Marshal(map[string]any{
			"category": r.URL.Query().Get("category"),
			"month":    r.URL.Query().Get("month"),
			"limit":    limit,
		})
		reply, err := s.Discovery.ListHotMerchants(r.Context(), &discoveryclient.JsonBody{BodyJson: body})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func CreateMerchant(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.CreateMerchant(rpcContext(r), &discoveryclient.IdBody{BodyJson: body})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func GetMerchant(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply, err := s.Discovery.GetMerchantAdmin(rpcContext(r), &discoveryclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func UpdateMerchant(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.UpdateMerchant(rpcContext(r), &discoveryclient.IdBody{Id: pathvar.Vars(r)["id"], BodyJson: body})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func DeleteMerchant(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.Discovery.ArchiveMerchant(rpcContext(r), &discoveryclient.IdRequest{Id: pathvar.Vars(r)["id"]}); err != nil {
			writeDiscoveryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func TaskReviewTarget(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply, err := s.Discovery.MerchantReviewTarget(rpcContext(r), &discoveryclient.IdBody{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func CreateTaskMerchantReview(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.CreateTaskMerchantReview(rpcContext(r), &discoveryclient.IdBody{Id: pathvar.Vars(r)["id"], BodyJson: body})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func ListMyMerchantReviews(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := pageQuery(r)
		reply, err := s.Discovery.ListMyMerchantReviews(rpcContext(r), &discoveryclient.ListQuery{Limit: limit, Offset: offset})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func ListPlaceReviewsAdmin(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := pageQuery(r)
		reply, err := s.Discovery.ListPlaceReviewsAdmin(rpcContext(r), &discoveryclient.AdminReviewQuery{
			Source: r.URL.Query().Get("source"), PlaceId: r.URL.Query().Get("place_id"), Limit: limit, Offset: offset,
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func RevealPlaceReviewIdentity(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.RevealPlaceReviewIdentity(rpcContext(r), &discoveryclient.IdBody{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		var revealed struct {
			ReviewID string `json:"review_id"`
			UserID   string `json:"user_id"`
			Action   string `json:"action"`
			Reason   string `json:"reason"`
		}
		if err := json.Unmarshal(reply.GetBodyJson(), &revealed); err != nil || revealed.UserID == "" {
			httpkit.WriteError(w, http.StatusInternalServerError, "invalid reveal response")
			return
		}
		if s.Identity == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "identity service unavailable")
			return
		}
		profileReply, err := s.Identity.GetUserAdmin(rpcContext(r), &identityclient.IdRequest{Id: revealed.UserID})
		if err != nil {
			httpkit.WriteError(w, http.StatusBadGateway, "identity profile unavailable")
			return
		}
		var profile struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Phone string `json:"phone"`
		}
		if err := json.Unmarshal(profileReply.GetBodyJson(), &profile); err != nil {
			httpkit.WriteError(w, http.StatusInternalServerError, "invalid identity response")
			return
		}
		writeRawJSON(w, http.StatusOK, mustJSON(map[string]any{
			"review_id": revealed.ReviewID, "action": revealed.Action,
			"user": map[string]string{"id": profile.ID, "name": profile.Name, "phone": profile.Phone},
		}))
	}
}

func pageQuery(r *http.Request) (int32, int32) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return int32(limit), int32(offset)
}

func writeDiscoveryError(w http.ResponseWriter, err error) {
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "authentication required",
		Forbidden:       "operation is not allowed",
		NotFound:        "resource not found",
		Conflict:        "operation conflict",
		BadRequest:      "invalid request",
		Unavailable:     "discovery service unavailable",
		Internal:        "could not process discovery request",
	})
}
