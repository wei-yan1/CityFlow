package handler

import (
	"net/http"
	"strconv"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/app/gateway/cmd/api/internal/svc"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

func ListPlaceComments(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := queryLimit(r)
		reply, err := s.Discovery.ListPlaceComments(rpcContext(r), &discoveryclient.CommentListRequest{
			ReviewId: pathvar.Vars(r)["id"], Cursor: r.URL.Query().Get("cursor"), Limit: limit,
			Sort: r.URL.Query().Get("sort"),
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func ListPlaceCommentThread(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := queryThreadLimit(r)
		reply, err := s.Discovery.ListPlaceCommentThread(rpcContext(r), &discoveryclient.CommentThreadRequest{
			ReviewId: pathvar.Vars(r)["id"], RootId: pathvar.Vars(r)["rootId"], Limit: limit,
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func CreatePlaceComment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Discovery.CreatePlaceComment(rpcContext(r), &discoveryclient.CommentRequest{
			ReviewId: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func HidePlaceComment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := s.Discovery.HidePlaceComment(rpcContext(r), &discoveryclient.CommentRequest{
			ReviewId: pathvar.Vars(r)["id"], ParentId: pathvar.Vars(r)["commentId"],
		})
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func ListRiderComments(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply, err := s.Task.ListRiderComments(rpcContext(r), &taskclient.CommentListRequest{
			FeedbackId: pathvar.Vars(r)["id"], Cursor: r.URL.Query().Get("cursor"), Limit: queryLimit(r),
			Sort: r.URL.Query().Get("sort"),
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func ListRiderCommentThread(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply, err := s.Task.ListRiderCommentThread(rpcContext(r), &taskclient.CommentThreadRequest{
			FeedbackId: pathvar.Vars(r)["id"], RootId: pathvar.Vars(r)["rootId"], Limit: queryThreadLimit(r),
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func CreateRiderComment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Task.CreateRiderComment(rpcContext(r), &taskclient.CommentRequest{
			FeedbackId: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func HideRiderComment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := s.Task.HideRiderComment(rpcContext(r), &taskclient.CommentRequest{
			FeedbackId: pathvar.Vars(r)["id"], ParentId: pathvar.Vars(r)["commentId"],
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func queryLimit(r *http.Request) int32 {
	return queryIntLimit(r, 50)
}

func queryThreadLimit(r *http.Request) int32 {
	return queryIntLimit(r, 100)
}

func queryIntLimit(r *http.Request, max int32) int32 {
	value, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if value <= 0 {
		return 20
	}
	if value > int(max) {
		return max
	}
	return int32(value)
}
