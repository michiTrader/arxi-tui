package extv2

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpSeams are the places a test replaces the network. In production every field is
// nil and the real resolver, dialer and trust store are used; the address checks run
// either way, so a test that redirects the dial still proves the guard.
type httpSeams struct {
	lookup func(ctx context.Context, host string) ([]net.IP, error)
	dial   func(ctx context.Context, network, addr string) (net.Conn, error)
	tls    *tls.Config
}

// ErrBlockedAddress is returned when a name resolves to an address the package was not
// allowed to reach.
var ErrBlockedAddress = errors.New("blocked address")

// queryEscape escapes a typed value for the inside of a URL: every reserved character
// is percent-encoded, so a value can fill a path segment or a query value and can never
// add a parameter, a path or a fragment of its own. A space becomes %20 (the "+" of the
// query form would be a literal plus in a path).
func queryEscape(v string) string {
	return strings.ReplaceAll(url.QueryEscape(v), "+", "%20")
}

// forbiddenAddr reports why an address may not be reached by an operation that has no
// local permission: loopback, private, link-local (the metadata service lives there),
// unspecified, multicast, and the carrier-grade range. An IPv4 address wrapped in IPv6
// is judged as the IPv4 address it is.
func forbiddenAddr(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsLoopback():
		return "loopback (this machine)"
	case ip.IsPrivate():
		return "private network"
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return "link-local"
	case ip.IsUnspecified():
		return "unspecified"
	case ip.IsMulticast():
		return "multicast"
	case ip.IsInterfaceLocalMulticast():
		return "interface-local"
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 64 {
		return "carrier-grade shared range"
	}
	return ""
}

func (s httpSeams) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if s.lookup != nil {
		return s.lookup(ctx, host)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

// guardedDialer resolves the name itself, judges every address it got, and dials one
// of those addresses directly. Dialing the checked address (never the name again) is
// what closes the gap where a second lookup answers differently.
func (s httpSeams) guardedDialer(allowLocal bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := s.resolve(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("the host %q could not be looked up (%v); check the name and your connection", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("the host %q has no address; check the name", host)
		}
		if !allowLocal {
			for _, ip := range ips {
				if why := forbiddenAddr(ip); why != "" {
					return nil, fmt.Errorf("%w: %q points at %s (%s), which a public host entry cannot reach; if it really is on your machine or network, declare it as local:%s:%s", ErrBlockedAddress, host, ip, why, host, port)
				}
			}
		}
		var last error
		for _, ip := range ips {
			target := net.JoinHostPort(ip.String(), port)
			var c net.Conn
			if s.dial != nil {
				c, err = s.dial(ctx, network, target)
			} else {
				var d net.Dialer
				c, err = d.DialContext(ctx, network, target)
			}
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}
}

// ErrRedirect is returned for a 3xx answer. A redirect is how an allowed host sends the
// request somewhere the user never listed, so it is an answer the operation does not follow.
var ErrRedirect = errors.New("redirect refused")

func (r *Runner) newClient(target netPerm) *http.Client {
	tr := &http.Transport{
		// No proxy: a proxy would make the connection itself, so the checks on the
		// address would be checks on the proxy's.
		Proxy:                  nil,
		DialContext:            r.http.guardedDialer(target.local),
		TLSClientConfig:        r.http.tls,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  0,
		MaxResponseHeaderBytes: 16 << 10,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      true,
	}
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func timeoutFor(o Op, oc opCtx) time.Duration {
	ms := o.TimeoutMS
	if ms == 0 {
		if oc.kind == ctxRule {
			ms = DefaultRuleTimeoutMS
		} else {
			ms = DefaultCommandTimeoutMS
		}
	}
	return time.Duration(ms) * time.Millisecond
}

func (r *Runner) runHTTP(ctx context.Context, p *Package, o Op, oc opCtx, in Input) (Outcome, error) {
	urlParts, err := parseTemplate(o.URL)
	if err != nil {
		return Outcome{}, err
	}
	target, err := p.checkURLTarget("http", urlParts)
	if err != nil {
		return Outcome{}, err
	}
	for _, v := range []string{in.Args, in.Line} {
		if strings.ContainsRune(v, 0) {
			return Outcome{}, fmt.Errorf("the text holds a NUL byte, which a request cannot carry; remove it")
		}
	}
	rawURL := expand(urlParts, in, queryEscape)
	u, err := url.Parse(rawURL)
	if err != nil {
		return Outcome{}, fmt.Errorf("the request address is malformed after filling in your text (%v); try different text", firstLine(err.Error()))
	}
	// The address the request would really go to must be the one the package declared.
	// Validation proved it for the literal part and escaping keeps a typed value out of
	// the host; this proves it for the whole, so a future change to either cannot turn
	// into a request to somewhere the user never listed.
	if !sameTarget(u, target) {
		return Outcome{}, fmt.Errorf("the request address changed where it points after filling in your text, so it was not sent; this is refused and nothing was contacted")
	}

	method := o.Method
	if method == "" {
		method = "GET"
	}
	var body io.Reader
	contentType := ""
	if o.Body != "" {
		bp, err := parseTemplate(o.Body)
		if err != nil {
			return Outcome{}, err
		}
		enc := o.BodyEncoding
		if enc == "" {
			enc = "json"
		}
		var s string
		switch enc {
		case "json":
			s, contentType = expand(bp, in, jsonEscape), "application/json"
		case "form":
			s, contentType = expand(bp, in, queryEscape), "application/x-www-form-urlencoded"
		default:
			s, contentType = expand(bp, in, nil), "text/plain; charset=utf-8"
		}
		body = strings.NewReader(s)
	}

	ctx, cancel := context.WithTimeout(ctx, timeoutFor(o, oc))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return Outcome{}, fmt.Errorf("the request could not be built (%v)", firstLine(err.Error()))
	}
	req.Header.Set("User-Agent", "arxi-tui-ext/2")
	req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.1")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range o.Headers {
		req.Header.Set(k, v)
	}

	client := r.newClient(target.perm)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return Outcome{}, httpFailure(ctx, u.Hostname(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return Outcome{}, fmt.Errorf("%w: %s answered %d and asked to go elsewhere; an allowed host cannot send the request to one that was not listed. Use the final address in the package", ErrRedirect, u.Hostname(), resp.StatusCode)
	}
	data, truncated, err := readLimited(resp.Body, MaxOutputBytes)
	if err != nil {
		return Outcome{}, httpFailure(ctx, u.Hostname(), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Outcome{}, fmt.Errorf("%s answered %d %s: %s", u.Hostname(), resp.StatusCode, CleanText(http.StatusText(resp.StatusCode), 60, 1), CleanText(string(data), 160, 1))
	}
	return finish(o, oc, data, truncated)
}

// sameTarget reports whether u still goes to the scheme, host and port the package
// declared, with no credentials in front of the host.
func sameTarget(u *url.URL, t urlTarget) bool {
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	return u.User == nil && strings.ToLower(u.Hostname()) == t.perm.host && port == t.perm.port && u.Scheme == t.scheme
}

func httpFailure(ctx context.Context, host string, err error) error {
	switch {
	case errors.Is(err, ErrBlockedAddress):
		return err
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s did not answer in time, so the request was dropped; try again, or raise timeout_ms (at most %d)", host, MaxCommandTimeoutMS)
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf("the request to %s was cancelled", host)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("%s could not be reached (%s); check your connection and the address in the package", host, firstLine(err.Error()))
}

// readLimited reads at most max bytes and reports whether there was more.
func readLimited(r io.Reader, max int) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > max {
		return data[:max], true, nil
	}
	return data, false, nil
}
