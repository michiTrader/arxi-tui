package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// webRig runs one scripted web_fetch call under a web policy.
type webRig struct {
	f     *fakeLLM
	got   []chatToolNotification
	asked []chatApprovalNotification
	err   error
}

func runWeb(t *testing.T, web string, ask func(chatApprovalNotification) (bool, error), args string) *webRig {
	t.Helper()
	r := &webRig{f: setUpFake(t)}
	r.f.script = []scripted{{name: "web_fetch", args: args}}
	wrapped := ask
	if ask != nil {
		wrapped = func(n chatApprovalNotification) (bool, error) {
			r.asked = append(r.asked, n)
			return ask(n)
		}
	}
	ctx, err := withTools(context.Background(), projectDir(t), "", wrapped, func(n chatToolNotification) { r.got = append(r.got, n) })
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = withWeb(ctx, web); err != nil {
		t.Fatal(err)
	}
	_, r.err = chatSendEffort(ctx, "look it up", "", "", "", "")
	return r
}

func TestWebIsNotOfferedUnlessAllowed(t *testing.T) {
	r := runWeb(t, "", nil, `{"url":"https://example.com"}`)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if got := strings.Join(r.f.tools[0], ","); got != "list,read,grep" {
		t.Errorf("offered tools = %q; the web must not be offered by default", got)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "not available") {
		t.Errorf("a web call nobody offered must be refused: %+v", r.got)
	}
}

func TestWebAskOffersTheToolAndAsksFirst(t *testing.T) {
	r := runWeb(t, editsAsk, func(chatApprovalNotification) (bool, error) { return false, nil }, `{"url":"https://example.com/docs"}`)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if got := strings.Join(r.f.tools[0], ","); got != "list,read,grep,web_fetch" {
		t.Errorf("offered tools = %q", got)
	}
	if len(r.asked) != 1 || r.asked[0].Name != "web_fetch" || r.asked[0].Arg != "https://example.com/docs" ||
		!strings.Contains(r.asked[0].Summary, "example.com") {
		t.Fatalf("the user must be asked about the address: %+v", r.asked)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "did not allow") {
		t.Errorf("a declined page must be reported as a failure: %+v", r.got)
	}
}

func TestWebAskWithoutAWayToAskIsRefused(t *testing.T) {
	r := runWeb(t, editsAsk, nil, `{"url":"https://example.com"}`)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "approval") {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestWebNeverReachesTheLocalMachineEvenWhenAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the local server was reached")
	}))
	defer srv.Close()
	r := runWeb(t, editsAllow, nil, `{"url":"`+srv.URL+`"}`)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.asked) != 0 {
		t.Errorf("allow must not ask: %+v", r.asked)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "own machine or network") {
		t.Errorf("a local address must be refused even under allow: %+v", r.got)
	}
}

func TestWebBadArgumentsAreReadByTheModel(t *testing.T) {
	r := runWeb(t, editsAllow, nil, `{"url":"  "}`)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "needs a url") {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestWebNeedsAWorkdirAndAValidPolicy(t *testing.T) {
	if _, err := withWeb(context.Background(), editsAsk); err == nil {
		t.Error("web without tools must be refused")
	}
	ctx, _ := collect(t, projectDir(t))
	if _, err := withWeb(ctx, "yolo"); err == nil {
		t.Error("web must be deny, ask or allow")
	}
}

func TestTheWebHintOnlyAppearsWithTheWeb(t *testing.T) {
	if webHint(editsDeny) != "" || webHint("") != "" {
		t.Error("no web, no hint")
	}
	if !strings.Contains(webHint(editsAsk), "untrusted") {
		t.Error("the hint must say pages are untrusted")
	}
}
