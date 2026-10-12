package extv2

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// netFixture serves a handler over TLS and returns a Runner whose network is
// redirected to it. The name the package uses ("api.example.com") resolves to a
// public-looking address, so the address guard runs exactly as in production; only the
// final dial is pointed at the test server.
type netFixture struct {
	r      *Runner
	srv    *httptest.Server
	dialed atomic.Int32
	looked []string
}

func newNetFixture(t *testing.T, h http.Handler, resolveTo string) *netFixture {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	f := &netFixture{srv: srv, r: &Runner{}}
	f.r.http = httpSeams{
		tls: &tls.Config{RootCAs: pool, ServerName: "example.com"},
		lookup: func(_ context.Context, host string) ([]net.IP, error) {
			f.looked = append(f.looked, host)
			return []net.IP{net.ParseIP(resolveTo)}, nil
		},
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			f.dialed.Add(1)
			var d net.Dialer
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}
	return f
}

const publicIP = "93.184.216.34"

func httpPkg(t *testing.T, op string, extra string) (*Package, Command) {
	t.Helper()
	p := accepted(t, `{`+head+`,"permissions":{"net":["api.example.com"]`+extra+`},"commands":[{"name":"a","min_args":0,"op":`+op+`}]}`, Options{})
	return p, p.Commands[0]
}

func TestHTTPReturnsCleanedText(t *testing.T) {
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "sunny \x1b[2J\x1b]0;pwn\x07 and 21C\r\nline two\u202e")
	}), publicIP)
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/w?q=${args}"}`, "")
	out, err := f.r.RunCommand(context.Background(), p, c, "Paris")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.Say, "\x1b\x07\r\u202e") {
		t.Fatalf("output was not cleaned: %q", out.Say)
	}
	if out.Say != "sunny  and 21C\nline two" {
		t.Fatalf("Say = %q", out.Say)
	}
}

func TestHTTPEscapesTypedTextSoItCannotAddParameters(t *testing.T) {
	var got string
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		io.WriteString(w, "ok")
	}), publicIP)
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/w?q=${args}"}`, "")
	if _, err := f.r.RunCommand(context.Background(), p, c, "a&admin=1#x y"); err != nil {
		t.Fatal(err)
	}
	if got != "q=a%26admin%3D1%23x%20y" {
		t.Fatalf("query = %q, the typed text must be one value", got)
	}
}

func TestHTTPTypedTextCannotChangeTheHost(t *testing.T) {
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }), publicIP)
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/${args}"}`, "")
	for _, evil := range []string{"@evil.com/", "/../..", "\\@evil.com", "x\r\nHost: evil.com", "..%2f..%2f", "//evil.com/x"} {
		f.dialed.Store(0)
		f.looked = nil
		_, _ = f.r.RunCommand(context.Background(), p, c, evil)
		for _, h := range f.looked {
			if h != "api.example.com" {
				t.Errorf("text %q made the runner look up %q", evil, h)
			}
		}
	}
}

func TestHTTPBlocksPrivateAddressesBehindAPublicName(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.5", "192.168.1.1", "172.16.0.1", "169.254.169.254", "0.0.0.0", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "100.64.0.1", "224.0.0.1"} {
		t.Run(ip, func(t *testing.T) {
			f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secret") }), ip)
			p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/"}`, "")
			_, err := f.r.RunCommand(context.Background(), p, c, "")
			if !errors.Is(err, ErrBlockedAddress) {
				t.Fatalf("got %v, want ErrBlockedAddress", err)
			}
			if f.dialed.Load() != 0 {
				t.Fatal("a blocked address must never be dialled")
			}
			if !strings.Contains(err.Error(), "local:") {
				t.Fatalf("the message must say how to declare a local service: %v", err)
			}
		})
	}
}

func TestHTTPBlocksWhenAnyAnswerIsPrivate(t *testing.T) {
	// A name with one public and one private answer must not be reachable: choosing
	// among the answers is the attacker's to influence.
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "x") }), publicIP)
	f.r.http.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP(publicIP), net.ParseIP("10.1.1.1")}, nil
	}
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/"}`, "")
	if _, err := f.r.RunCommand(context.Background(), p, c, ""); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPLocalPermissionReachesLocalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "local ok") }))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	p := accepted(t, `{`+head+`,"permissions":{"net":["local:`+host+`:`+port+`"]},"commands":[{"name":"a","op":{"kind":"http","url":"http://`+host+`:`+port+`/"}}]}`, Options{})
	out, err := (&Runner{}).RunCommand(context.Background(), p, p.Commands[0], "")
	if err != nil || out.Say != "local ok" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestHTTPRefusesRedirects(t *testing.T) {
	var hit atomic.Int32
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
		http.Redirect(w, r, "https://evil.example.net/steal", http.StatusFound)
	}), publicIP)
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/"}`, "")
	_, err := f.r.RunCommand(context.Background(), p, c, "")
	if !errors.Is(err, ErrRedirect) {
		t.Fatalf("got %v", err)
	}
	if hit.Load() != 1 || f.dialed.Load() != 1 {
		t.Fatalf("a redirect must not be followed (hits=%d dials=%d)", hit.Load(), f.dialed.Load())
	}
}

func TestHTTPStatusErrorsAreReportedWithConsequence(t *testing.T) {
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, "boom \x1b[31m")
	}), publicIP)
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/"}`, "")
	_, err := f.r.RunCommand(context.Background(), p, c, "")
	if err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPTimeoutAndSizeLimit(t *testing.T) {
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}), publicIP)
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/","timeout_ms":200}`, "")
	start := time.Now()
	_, err := f.r.RunCommand(context.Background(), p, c, "")
	if err == nil || !strings.Contains(err.Error(), "did not answer in time") {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the timeout did not bound the call")
	}

	big := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", MaxOutputBytes*3))
	}), publicIP)
	out, err := big.r.RunCommand(context.Background(), p, c, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Say) > MaxSayRunes+4 || !strings.HasSuffix(out.Say, "…") {
		t.Fatalf("say is %d bytes, want it cut to %d runes", len(out.Say), MaxSayRunes)
	}
}

func TestHTTPPostBodiesAreEscapedPerEncoding(t *testing.T) {
	var gotBody, gotType string
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotType = string(b), r.Header.Get("Content-Type")
		io.WriteString(w, "ok")
	}), publicIP)
	args := `he said "hi"` + "\n" + `{"x":1}`
	p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/","method":"POST","body":"{\"q\":\"${args}\"}"}`, "")
	if _, err := f.r.RunCommand(context.Background(), p, c, args); err != nil {
		t.Fatal(err)
	}
	if gotType != "application/json" || gotBody != `{"q":"he said \"hi\"\n{\"x\":1}"}` {
		t.Fatalf("json body = %q (%s)", gotBody, gotType)
	}
	p, c = httpPkg(t, `{"kind":"http","url":"https://api.example.com/","method":"POST","body_encoding":"form","body":"q=${args}&k=1"}`, "")
	if _, err := f.r.RunCommand(context.Background(), p, c, "a&k=2"); err != nil {
		t.Fatal(err)
	}
	if gotType != "application/x-www-form-urlencoded" || gotBody != "q=a%26k%3D2&k=1" {
		t.Fatalf("form body = %q (%s)", gotBody, gotType)
	}
}

func TestHTTPExtractReadsOneValue(t *testing.T) {
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"current":{"temp":21.5,"sky":"clear"},"items":[{"name":"a"},{"name":"b"}],"nothing":null}`)
	}), publicIP)
	for path, want := range map[string]string{"current.temp": "21.5", "current.sky": "clear", "items.1.name": "b"} {
		p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/","extract":"`+path+`"}`, "")
		out, err := f.r.RunCommand(context.Background(), p, c, "")
		if err != nil || out.Say != want {
			t.Errorf("extract %s: out=%q err=%v want %q", path, out.Say, err, want)
		}
	}
	for path, want := range map[string]string{"current": "not a single value", "current.nope": "keys there are", "items.9.name": "not a position", "nothing": "null", "current.temp.x": "single value"} {
		p, c := httpPkg(t, `{"kind":"http","url":"https://api.example.com/","extract":"`+path+`"}`, "")
		_, err := f.r.RunCommand(context.Background(), p, c, "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("extract %s: err=%v want %q", path, err, want)
		}
	}
}

func TestRunnerRechecksThePermissionOfAnUnvalidatedDocument(t *testing.T) {
	// A package that skipped Validate must still not reach a host it did not declare.
	f := newNetFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "x") }), publicIP)
	p := &Package{Protocol: Protocol, ID: "demo", Name: "d", Version: "1"}
	c := Command{Name: "a", Op: Op{Kind: KindHTTP, URL: "https://api.example.com/"}}
	if _, err := f.r.RunCommand(context.Background(), p, c, ""); err == nil || !strings.Contains(err.Error(), "permissions.net") {
		t.Fatalf("got %v", err)
	}
	if f.dialed.Load() != 0 {
		t.Fatal("nothing may be dialled for an undeclared host")
	}
}

func TestForbiddenAddrTable(t *testing.T) {
	for ip, bad := range map[string]bool{
		"8.8.8.8": false, "93.184.216.34": false, "2606:4700::1111": false,
		"127.0.0.1": true, "127.1.2.3": true, "10.0.0.1": true, "172.31.255.255": true, "192.168.0.1": true,
		"169.254.169.254": true, "0.0.0.0": true, "::": true, "::1": true, "fe80::1": true, "fd00::1": true,
		"::ffff:10.0.0.1": true, "::ffff:8.8.8.8": false, "100.64.0.1": true, "100.127.255.255": true, "100.128.0.1": false,
		"224.0.0.251": true, "ff02::1": true,
	} {
		if got := forbiddenAddr(net.ParseIP(ip)) != ""; got != bad {
			t.Errorf("forbiddenAddr(%s) = %v, want %v", ip, got, bad)
		}
	}
}

func TestSameTarget(t *testing.T) {
	want := urlTarget{scheme: "https", perm: netPerm{host: "api.example.com", port: "443"}}
	for raw, ok := range map[string]bool{
		"https://api.example.com/x?q=1":      true,
		"https://API.example.com:443/x":      true,
		"https://api.example.com@evil.com/x": false,
		"https://user@api.example.com/x":     false,
		"https://evil.com/x":                 false,
		"https://api.example.com.evil.com/x": false,
		"https://api.example.com:8443/x":     false,
		"http://api.example.com/x":           false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := sameTarget(u, want); got != ok {
			t.Errorf("sameTarget(%s) = %v, want %v", raw, got, ok)
		}
	}
}
