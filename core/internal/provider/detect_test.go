package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/turn"
)

const wallPage = "<!DOCTYPE html><html><head><title>Attention Required! | Cloudflare</title></head><body>Sorry, you have been blocked</body></html>"

// host answers /v1/chat/completions and /v1/messages the way the test asks.
func host(t *testing.T, chat, msgs http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if chat != nil {
		mux.HandleFunc("/v1/chat/completions", chat)
	}
	if msgs != nil {
		mux.HandleFunc("/v1/messages", msgs)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func wall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(403)
	io.WriteString(w, wallPage)
}

func apiComplaint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(400)
	io.WriteString(w, `{"error":{"message":"missing fields"}}`)
}

func TestDetectProtocolReadsWhichRouteIsReachable(t *testing.T) {
	bare403 := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }
	cases := []struct {
		name       string
		chat, msgs http.HandlerFunc
		want       string
	}{
		{"openai only", apiComplaint, nil, model.ProtocolOpenAIChatCompletions},
		{"both open: openai wins", apiComplaint, apiComplaint, model.ProtocolOpenAIChatCompletions},
		{"openai walled, anthropic open", wall, apiComplaint, model.ProtocolAnthropicMessages},
		{"anthropic answers a bare 403", wall, bare403, model.ProtocolAnthropicMessages},
		{"both walled", wall, wall, ""},
		{"neither exists", nil, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := host(t, c.chat, c.msgs)
			if got := DetectProtocol(context.Background(), srv.URL+"/v1", "k", srv.Client()); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestDetectProtocolOnAnUnreachableHostSaysNothing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if got := DetectProtocol(context.Background(), url+"/v1", "k", nil); got != "" {
		t.Fatalf("a dead host produced %q; a flaky network must not choose a wire", got)
	}
}

func TestDetectEndpointAddsV1OnlyWhereTheRootAnswers(t *testing.T) {
	srv := host(t, wall, apiComplaint)
	base, proto := DetectEndpoint(context.Background(), srv.URL, "k", srv.Client())
	if base != srv.URL+"/v1" || proto != model.ProtocolAnthropicMessages {
		t.Fatalf("got %q %q", base, proto)
	}
	// a versioned URL is never altered
	base, _ = DetectEndpoint(context.Background(), srv.URL+"/v1/", "k", srv.Client())
	if base != srv.URL+"/v1" {
		t.Fatalf("versioned URL became %q", base)
	}
	// nothing answers anywhere
	empty := host(t, nil, nil)
	if b, p := DetectEndpoint(context.Background(), empty.URL, "k", empty.Client()); b != "" || p != "" {
		t.Fatalf("got %q %q for a host with no API", b, p)
	}
}

func anthropicReq(base string) turn.Request {
	return turn.Request{Schema: turn.Schema, Provider: "retrytest", Protocol: model.ProtocolAnthropicMessages,
		BaseURL: base, Model: "m", MaxTokens: 16,
		Messages: []turn.Message{{Role: turn.RoleUser, Content: []turn.ContentBlock{{Type: turn.BlockText, Text: "ping"}}}}}
}

func TestAnEmptyGatewayForbiddenIsRepeatedAndAThinkingBlockIsIgnored(t *testing.T) {
	old := gatewayRetryWait
	gatewayRetryWait = func(int) time.Duration { return time.Millisecond }
	defer func() { gatewayRetryWait = old }()

	var calls atomic.Int32
	srv := host(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","type":"message","role":"assistant","model":"m","stop_reason":"end_turn",
"content":[{"type":"thinking","thinking":"hm","signature":"s"},{"type":"text","text":"pong"}],
"usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	x := &Executor{}
	resp, err := x.CompleteTurn(context.Background(), anthropicReq(srv.URL+"/v1"))
	if err != nil || resp.FinishReason == turn.FinishRefusal {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3 (two empty 403s then success)", calls.Load())
	}
	var text strings.Builder
	for _, b := range resp.Content {
		text.WriteString(b.Text)
	}
	if text.String() != "pong" {
		t.Fatalf("text = %q; the thinking block must not leak or break the reply", text.String())
	}
}

func TestAForbiddenWithAnErrorBodyIsNotRepeated(t *testing.T) {
	var calls atomic.Int32
	srv := host(t, nil, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"type":"permission_error","message":"key disabled"}}`)
	})
	resp, _ := (&Executor{}).CompleteTurn(context.Background(), anthropicReq(srv.URL+"/v1"))
	if resp.FinishReason != turn.FinishRefusal || calls.Load() != 1 {
		t.Fatalf("finish=%v calls=%d; a real authorization failure explains itself and is not retried", resp.FinishReason, calls.Load())
	}
}

func TestAWalledWireFallsBackToTheOpenOneAndSaysSo(t *testing.T) {
	var chat atomic.Int32
	srv := host(t, func(w http.ResponseWriter, r *http.Request) { chat.Add(1); wall(w, r) },
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"1","type":"message","role":"assistant","model":"m","stop_reason":"end_turn",
"content":[{"type":"text","text":"pong"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
		})
	var gotProvider, gotWire string
	x := &Executor{OnWire: func(p, m, w string) { gotProvider, gotWire = p, w }}
	req := anthropicReq(srv.URL + "/v1")
	req.Protocol = model.ProtocolOpenAIChatCompletions
	resp, err := x.CompleteTurn(context.Background(), req)
	if err != nil || resp.FinishReason == turn.FinishRefusal {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if gotProvider != "retrytest" || gotWire != model.ProtocolAnthropicMessages {
		t.Fatalf("OnWire(%q,%q); the working wire must be remembered", gotProvider, gotWire)
	}
}
