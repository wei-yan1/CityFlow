package handler

import (
	"net/http"
	"strings"

	"cityflow/internal/platform/authctx"
	"cityflow/internal/platform/httpkit"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var protoJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}

func writeProtoJSON(w http.ResponseWriter, status int, message proto.Message) {
	body, err := protoJSON.Marshal(message)
	if err != nil {
		httpkit.WriteError(w, http.StatusInternalServerError, "could not encode response")
		return
	}
	writeRawJSON(w, status, body)
}

func requestUser(r *http.Request) (string, string, bool) {
	claims, ok := authctx.Claims(r.Context())
	if !ok || strings.TrimSpace(claims.Subject) == "" {
		return "", "", false
	}
	return claims.Subject, claims.Role, true
}
