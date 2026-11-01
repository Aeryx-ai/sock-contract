package contract_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	sockv1 "github.com/aeryx-ai/sock-contract/gen/go/sock/v1"
	"github.com/aeryx-ai/sock-contract/gen/go/sock/v1/sockv1connect"
)

type messaging struct {
	sockv1connect.UnimplementedMessagingServiceHandler
	got  *sockv1.NotifyOwnerRequest
	push *sockv1.RegisterPushRequest
}

func (m *messaging) NotifyOwner(_ context.Context, r *connect.Request[sockv1.NotifyOwnerRequest]) (*connect.Response[sockv1.NotifyOwnerResponse], error) {
	m.got = r.Msg
	return connect.NewResponse(&sockv1.NotifyOwnerResponse{Message: &sockv1.Message{Seq: 1}}), nil
}

func (m *messaging) RegisterPush(_ context.Context, r *connect.Request[sockv1.RegisterPushRequest]) (*connect.Response[sockv1.RegisterPushResponse], error) {
	m.push = r.Msg
	return connect.NewResponse(&sockv1.RegisterPushResponse{}), nil
}

func serve(t *testing.T) (*messaging, *httptest.Server) {
	t.Helper()
	m := &messaging{}
	mux := http.NewServeMux()
	mux.Handle(sockv1connect.NewMessagingServiceHandler(m))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return m, srv
}

func post(t *testing.T, srv *httptest.Server, procedure, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+procedure, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// The one-line curl a puppet runs must work: text only, no idempotency key.
func TestBareCurlNotifyOwner(t *testing.T) {
	m, srv := serve(t)
	code, body := post(t, srv, sockv1connect.MessagingServiceNotifyOwnerProcedure, `{"text":"build done 🧦"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	if m.got.GetText() != "build done 🧦" || m.got.GetIdempotencyKey() != "" {
		t.Fatalf("decoded %+v", m.got)
	}
}

// Pins current behavior for unknown JSON fields so clients know what to expect.
func TestUnknownJSONFieldIgnored(t *testing.T) {
	m, srv := serve(t)
	code, _ := post(t, srv, sockv1connect.MessagingServiceNotifyOwnerProcedure, `{"text":"x","bogus":1}`)
	if code != http.StatusOK {
		t.Fatalf("unknown field rejected (status %d); update api.md if this is intended", code)
	}
	if m.got.GetText() != "x" {
		t.Fatalf("known field lost alongside unknown one: text %q", m.got.GetText())
	}
}

// Shared fixture with the Swift test: bytes travel as standard base64.
const apnsTokenFixtureB64 = "3q2+7w=="

func TestApnsTokenBase64(t *testing.T) {
	m, srv := serve(t)
	code, body := post(t, srv, sockv1connect.MessagingServiceRegisterPushProcedure,
		`{"apnsToken":"`+apnsTokenFixtureB64+`","environment":"APNS_ENVIRONMENT_SANDBOX"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	if !bytes.Equal(m.push.GetApnsToken(), []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("token %x", m.push.GetApnsToken())
	}
	if m.push.GetEnvironment() != sockv1.ApnsEnvironment_APNS_ENVIRONMENT_SANDBOX {
		t.Fatalf("env %v", m.push.GetEnvironment())
	}
}

type operator struct {
	sockv1connect.UnimplementedOperatorServiceHandler
	resolve *sockv1.ResolveReportRequest
}

func (o *operator) ResolveReport(_ context.Context, r *connect.Request[sockv1.ResolveReportRequest]) (*connect.Response[sockv1.ResolveReportResponse], error) {
	o.resolve = r.Msg
	return connect.NewResponse(&sockv1.ResolveReportResponse{Resolution: &sockv1.Resolution{Outcome: r.Msg.GetOutcome()}}), nil
}

// The web console sends JSON from the browser: camelCase field names and enum
// value names, as Connect-Web encodes them.
func TestOperatorResolveReportJSON(t *testing.T) {
	o := &operator{}
	mux := http.NewServeMux()
	mux.Handle(sockv1connect.NewOperatorServiceHandler(o))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	code, body := post(t, srv, sockv1connect.OperatorServiceResolveReportProcedure,
		`{"reportId":"0199d1a2-0000-7000-8000-000000000001","outcome":"RESOLUTION_OUTCOME_OWNER_SUSPENDED"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	if o.resolve.GetReportId() != "0199d1a2-0000-7000-8000-000000000001" ||
		o.resolve.GetOutcome() != sockv1.ResolutionOutcome_RESOLUTION_OUTCOME_OWNER_SUSPENDED {
		t.Fatalf("decoded %+v", o.resolve)
	}
	if want := `"outcome":"RESOLUTION_OUTCOME_OWNER_SUSPENDED"`; !bytes.Contains([]byte(body), []byte(want)) {
		t.Fatalf("response %s lacks %s", body, want)
	}
}

// Read markers: int64 seqs travel as JSON strings, the marker rides on
// Conversation, and the stream event is a oneof case an old client can skip.
func TestReadMarkerWire(t *testing.T) {
	var req sockv1.MarkReadRequest
	if err := protojson.Unmarshal([]byte(`{"conversationId":"c1","readThroughSeq":"12"}`), &req); err != nil || req.GetReadThroughSeq() != 12 || req.GetConversationId() != "c1" {
		t.Fatalf("MarkReadRequest: %v %+v", err, &req)
	}
	b, err := protojson.Marshal(&sockv1.Conversation{Id: "c1", ReadThroughSeq: 7})
	if err != nil || !bytes.Contains(b, []byte(`"readThroughSeq":"7"`)) {
		t.Fatalf("Conversation JSON %s (%v)", b, err)
	}
	ev := &sockv1.SubscribeResponse{Event: &sockv1.SubscribeResponse_ReadMarker{ReadMarker: &sockv1.ReadMarker{ConversationId: "c1", ReadThroughSeq: 9}}}
	b, err = proto.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var back sockv1.SubscribeResponse
	if err := proto.Unmarshal(b, &back); err != nil || back.GetReadMarker().GetReadThroughSeq() != 9 || back.GetMessage() != nil || back.GetHeartbeat() != nil {
		t.Fatalf("SubscribeResponse round trip: %v %+v", err, &back)
	}
}

// Each oneof case of the archive record survives a wire round trip.
func TestHistoryRecordRoundTrip(t *testing.T) {
	records := map[string]*sockv1.HistoryRecord{
		"header": {Record: &sockv1.HistoryRecord_Header{Header: &sockv1.HistoryHeader{
			FormatVersion: 1,
			AccountId:     "9f6c1a52-3c1e-4d5b-8a7e-1b2c3d4e5f60",
			ExportedAt:    timestamppb.New(time.Unix(1_700_000_000, 0)),
		}}},
		"account": {Record: &sockv1.HistoryRecord_Account{Account: &sockv1.Account{Id: "a1", DisplayName: "Ada"}}},
		"conversation": {Record: &sockv1.HistoryRecord_Conversation{Conversation: &sockv1.Conversation{
			Id: "c1", ReadThroughSeq: 7,
		}}},
		"message": {Record: &sockv1.HistoryRecord_Message{Message: &sockv1.Message{
			Id: "m1", ConversationId: "c1", Seq: 7, EvidenceTag: []byte{1, 2, 3},
		}}},
	}
	for name, rec := range records {
		b, err := proto.Marshal(rec)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var got sockv1.HistoryRecord
		if err := proto.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if !proto.Equal(rec, &got) {
			t.Errorf("%s: round trip changed the record: %v != %v", name, rec, &got)
		}
	}
}

// The service has exactly the six RPCs of the spec, and only ReceiveHistory streams.
func TestHistoryTransferServiceDescriptor(t *testing.T) {
	svc := sockv1.File_sock_v1_history_transfer_proto.Services().ByName("HistoryTransferService")
	if svc == nil {
		t.Fatal("HistoryTransferService missing")
	}
	want := map[string]bool{
		"OpenHistoryTransfer": false, "GetHistoryTransferKey": false, "ClaimHistoryTransfer": false,
		"UploadHistoryChunk": false, "ReceiveHistory": true, "CloseHistoryTransfer": false,
	}
	if svc.Methods().Len() != len(want) {
		t.Errorf("got %d methods, want %d", svc.Methods().Len(), len(want))
	}
	for name, streams := range want {
		m := svc.Methods().ByName(protoreflect.Name(name))
		if m == nil {
			t.Errorf("%s missing", name)
			continue
		}
		if m.IsStreamingServer() != streams || m.IsStreamingClient() {
			t.Errorf("%s: server streaming = %v, client streaming = %v", name, m.IsStreamingServer(), m.IsStreamingClient())
		}
	}
}
