package discoveryrpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/internal/common/contract"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type fakeHotRankClient struct {
	body   string
	token  string
	reply  string
	err    error
	called bool
}

func (f *fakeHotRankClient) ListHotMerchants(ctx context.Context, in *discoveryclient.JsonBody, _ ...grpc.CallOption) (*discoveryclient.JsonReply, error) {
	f.called = true
	f.body = string(in.GetBodyJson())
	md, _ := metadata.FromOutgoingContext(ctx)
	values := md.Get(contract.InternalJobTokenHeader)
	if len(values) > 0 {
		f.token = values[0]
	}
	if f.err != nil {
		return nil, f.err
	}
	return &discoveryclient.JsonReply{BodyJson: []byte(f.reply)}, nil
}

func TestRankBuilderSendsRebuildAndToken(t *testing.T) {
	client := &fakeHotRankClient{reply: `{"month":"202611","categories":{"FOOD":2,"SUPPLIES":0,"OTHER":0},"items":2}`}
	builder := RankBuilder{Client: client, Token: "job-secret", Limit: 7}
	message, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !client.called || client.token != "job-secret" {
		t.Fatalf("rebuild request was not authenticated: %+v", client)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(client.body), &body); err != nil {
		t.Fatal(err)
	}
	if body["rebuild"] != true || body["limit"] != float64(7) {
		t.Fatalf("unexpected request body: %s", client.body)
	}
	if !strings.Contains(message, "month=202611") || !strings.Contains(message, "items=2") {
		t.Fatalf("unexpected result message: %s", message)
	}
}

func TestRankBuilderRequiresToken(t *testing.T) {
	_, err := (RankBuilder{Client: &fakeHotRankClient{reply: `{}`}}).Build(context.Background())
	if err == nil {
		t.Fatal("missing token should fail")
	}
}

func TestRankBuilderPropagatesRPCError(t *testing.T) {
	client := &fakeHotRankClient{err: errors.New("discovery unavailable")}
	_, err := (RankBuilder{Client: client, Token: "job-secret"}).Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "discovery unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}
