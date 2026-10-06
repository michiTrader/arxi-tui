package webtools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func allowAll(netip.Addr) bool { return false }

func TestBlockedAddr(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "127.1.2.3", "::1", "10.0.0.5", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "fe80::1", "fc00::1", "0.0.0.0", "::", "100.64.0.1", "224.0.0.1",
		"255.255.255.255", "198.18.0.1", "192.0.0.8", "240.0.0.1",
	} {
		if !blockedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s must be refused: it is on the user's machine or network", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "93.184.216.34", "1.1.1.1", "2606:4700:4700::1111", "172.32.0.1", "100.128.0.1"} {
		if blockedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s is a public address and must be allowed", s)
		}
	}
}

func TestFetchRefusesLoopbackEvenOnARealServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the server was reached; the dial must have been refused")
	}))
	defer srv.Close()
	_, err := NewFetcher().Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "own machine or network") {
		t.Fatalf("err = %v; want the own-machine refusal", err)
	}
}

func TestFetchRefusesARedirectIntoTheLocalNetwork(t *testing.T) {
	// The first hop is allowed (a stand-in for a public site); it redirects to a
	// metadata-style address, which the real rule must stop at connect time.
	rule := func(ip netip.Addr) bool { return ip.Is4() && ip.As4()[0] == 169 }
	hop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer hop.Close()
	_, err := newFetcher(rule).Fetch(context.Background(), hop.URL)
	if err == nil || !strings.Contains(err.Error(), "own machine or network") {
		t.Fatalf("err = %v; want the redirect refused", err)
	}
}

func TestFetchRefusesOddSchemesAndCredentials(t *testing.T) {
	f := newFetcher(allowAll)
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/x", "javascript:alert(1)", "http://user:pw@example.com/", ""} {
		if _, err := f.Fetch(context.Background(), u); err == nil {
			t.Errorf("Fetch(%q) succeeded; it must be refused", u)
		}
	}
}

func TestFetchHTMLBecomesReadableText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Hello &amp; welcome</title><style>p{color:red}</style></head>
<body><script>alert("x")</script><h1>Top</h1><p>One <a href="/two">two</a> three.</p>
<ul><li>a</li><li>b</li></ul><!-- hidden --></body></html>`))
	}))
	defer srv.Close()
	p, err := newFetcher(allowAll).Fetch(context.Background(), srv.URL+"/start")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Hello & welcome" {
		t.Errorf("title = %q", p.Title)
	}
	for _, want := range []string{"# Top", "One [two](" + srv.URL + "/two) three.", "- a", "- b"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, p.Text)
		}
	}
	for _, bad := range []string{"alert", "color:red", "hidden", "<"} {
		if strings.Contains(p.Text, bad) {
			t.Errorf("text must not contain %q:\n%s", bad, p.Text)
		}
	}
}

func TestFetchRejectsBinaryAndErrorsAndHugePages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4"))
	})
	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("word ", MaxBody)))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	f := newFetcher(allowAll)
	if _, err := f.Fetch(context.Background(), srv.URL+"/bin"); err == nil || !strings.Contains(err.Error(), "application/pdf") {
		t.Errorf("binary: err = %v", err)
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/404"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: err = %v", err)
	}
	p, err := f.Fetch(context.Background(), srv.URL+"/big")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Text) > MaxText || p.Cut == 0 || p.Bytes > MaxBody {
		t.Errorf("a huge page must be cut: text %d bytes, cut %d, downloaded %d", len(p.Text), p.Cut, p.Bytes)
	}
}

func TestFetchStopsARedirectLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := newFetcher(allowAll).Fetch(context.Background(), srv.URL+"/a"); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err = %v; want too many redirects", err)
	}
}

func TestFetchAddsHTTPSWhenTheSchemeIsMissing(t *testing.T) {
	_, err := newFetcher(allowAll).Fetch(context.Background(), "nonexistent.invalid/path")
	if err == nil {
		t.Fatal("expected an error for an unresolvable name")
	}
	if strings.Contains(err.Error(), "only http") {
		t.Fatalf("a bare host must be read as https, got %v", err)
	}
}

func TestDecodeLatin1(t *testing.T) {
	if got := decode([]byte{'c', 'a', 'f', 0xe9}, "iso-8859-1"); got != "café" {
		t.Errorf("decode = %q", got)
	}
	if got := decode([]byte{'a', 0xff, 'b'}, "utf-8"); !strings.Contains(got, "\uFFFD") {
		t.Errorf("invalid UTF-8 must be replaced, got %q", got)
	}
}

func TestHTMLToTextSurvivesBrokenMarkup(t *testing.T) {
	for _, src := range []string{
		"<p>unclosed <b>bold", "<a href=>x</a>", "<<<>>>", "<div", "<script>never closed", "<!-- never closed",
		"<a href='/x'>", "a < b and c > d", "<img alt=\"pic\">", "<title>t",
	} {
		htmlToText(src, nil) // must not panic or hang
	}
	_, got := htmlToText("a < b and c > d <img alt=\"pic\">", nil)
	if !strings.Contains(got, "a < b") || !strings.Contains(got, "[image: pic]") {
		t.Errorf("got %q", got)
	}
}

func TestHTMLToTextKeepsOnlyWebLinks(t *testing.T) {
	_, got := htmlToText(`<a href="javascript:evil()">js</a> <a href="mailto:a@b.c">mail</a> <a href="#top">top</a> <a href="https://ok.example/p#frag">ok</a>`, nil)
	if strings.Contains(got, "javascript") || strings.Contains(got, "mailto") {
		t.Errorf("unsafe links kept: %q", got)
	}
	if !strings.Contains(got, "[ok](https://ok.example/p)") {
		t.Errorf("web link missing or fragment kept: %q", got)
	}
}

func TestFetchTellsTheModelNotToWorkAroundABlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no bots", http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := newFetcher(allowAll).Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "Do not retry") {
		t.Errorf("a 403 must say so and tell the model to stop: %v", err)
	}
}
