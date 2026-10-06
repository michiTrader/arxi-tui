// Package webtools is what a chat turn may hand to the model so it can read the web: fetch
// a page as text, and search.
//
// A page is the one thing in the whole toolbox that the user did not write and the model
// cannot vouch for, so everything here assumes the worst of it. The fetcher refuses to
// reach the user's own machine or network (a page that redirects to http://localhost:8080
// or to a cloud metadata address is how a read-only tool becomes a way in), is bounded in
// time and size, never runs anything on the page, and labels what it returns as data. It
// is stdlib-only and works the same on Windows, macOS and Linux.
package webtools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Limits.
const (
	// MaxBody is how much of a response is read; the rest is dropped unread.
	MaxBody = 2 << 20
	// MaxText is how much page text goes back to the model.
	MaxText = 32 << 10
	// MaxRedirects bounds a chain of redirects.
	MaxRedirects = 5
	// Timeout bounds a whole fetch: connecting, waiting and reading.
	Timeout = 20 * time.Second
)

// UserAgent identifies the tool to the sites it visits.
const UserAgent = "arxi-tui/1 (+https://github.com/michiTrader/arxi-tui; a terminal assistant reading a page for its user)"

// Fetcher downloads pages. The zero value is not usable; use NewFetcher.
type Fetcher struct {
	client *http.Client
}

// NewFetcher returns a fetcher that will not connect to a private, loopback, link-local
// or otherwise internal address.
func NewFetcher() *Fetcher { return newFetcher(blockedAddr) }

// newFetcher builds the client around a refusal rule; tests pass one that lets a local
// test server through.
func newFetcher(blocked func(netip.Addr) bool) *Fetcher {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Control runs for every connection, after the name was resolved and with the
		// address about to be used. Checking here rather than before the request is what
		// makes redirects and DNS tricks (a name that answers with a public address to a
		// check and a private one to the connection) harmless.
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if blocked(ip.Unmap()) {
				return errBlocked
			}
			return nil
		},
	}
	tr := &http.Transport{
		// No proxy from the environment: a connection to a proxy would be checked
		// against the proxy's address, not the page's, and the rule above would mean
		// nothing.
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  15 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DisableKeepAlives:      true,
	}
	return &Fetcher{client: &http.Client{
		Transport: tr,
		Timeout:   Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return errors.New("too many redirects")
			}
			return checkURL(req.URL)
		},
	}}
}

var errBlocked = errors.New("that address is on the user's own machine or network, which the web tools never reach")

// blockedAddr reports whether an address is somewhere a web page must not send the user's
// machine: loopback, private ranges, link-local (which holds the cloud metadata service),
// shared-address space, multicast and the unspecified address.
func blockedAddr(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip.Is4() {
		b := ip.As4()
		switch {
		case b[0] == 100 && b[1]&0xc0 == 64: // 100.64.0.0/10, carrier-grade NAT
			return true
		case b[0] == 192 && b[1] == 0 && b[2] == 0: // 192.0.0.0/24, protocol assignments
			return true
		case b[0] == 198 && (b[1] == 18 || b[1] == 19): // 198.18.0.0/15, benchmarking
			return true
		case b[0] >= 240: // reserved and broadcast
			return true
		}
	}
	return false
}

// checkURL refuses anything that is not a plain http(s) address.
func checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https addresses can be read, not %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("the address has no host")
	}
	if u.User != nil {
		return errors.New("addresses with a user name or password in them are not read")
	}
	return nil
}

// Page is what a fetch returned.
type Page struct {
	// URL is where the content finally came from, after redirects.
	URL string
	// Title is the page's title, when it has one.
	Title string
	// Text is the readable content, cut to MaxText.
	Text string
	// Type is the content type the server declared.
	Type string
	// Cut is how many bytes of text did not fit; zero when all of it did.
	Cut int
	// Bytes is how much was downloaded.
	Bytes int
}

// Fetch downloads the page at rawURL and returns it as text.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("no address given")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("that is not an address: %w", err)
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	// The model gets the same cut as the user would: a reasonable time, not forever.
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json,text/markdown;q=0.9,*/*;q=0.1")
	req.Header.Set("Accept-Language", "en,es;q=0.8,*;q=0.5")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, simplify(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("the site answered %s", resp.Status)
	}
	ctype, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	kind := classify(ctype)
	if kind == kindBinary {
		return nil, fmt.Errorf("that is %s, not text, so it cannot be read", orUnknown(ctype))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, simplify(err)
	}
	src := decode(body, params["charset"])
	page := &Page{URL: resp.Request.URL.String(), Type: ctype, Bytes: len(body)}
	if kind == kindHTML || (kind == kindUnknown && looksLikeHTML(src)) {
		page.Title, page.Text = htmlToText(src, resp.Request.URL)
	} else {
		page.Text = strings.TrimSpace(src)
	}
	page.Text, page.Cut = cutText(page.Text, MaxText)
	return page, nil
}

const (
	kindText = iota
	kindHTML
	kindBinary
	kindUnknown
)

func classify(ctype string) int {
	switch {
	case ctype == "":
		return kindUnknown
	case ctype == "text/html" || ctype == "application/xhtml+xml":
		return kindHTML
	case strings.HasPrefix(ctype, "text/"), strings.HasSuffix(ctype, "+json"), strings.HasSuffix(ctype, "+xml"),
		ctype == "application/json", ctype == "application/xml", ctype == "application/javascript",
		ctype == "application/x-yaml", ctype == "application/yaml", ctype == "application/toml":
		return kindText
	}
	return kindBinary
}

func orUnknown(s string) string {
	if s == "" {
		return "an unknown type"
	}
	return s
}

func looksLikeHTML(s string) bool {
	head := strings.ToLower(s[:min(len(s), 1024)])
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html") || strings.Contains(head, "<body")
}

// decode makes the body valid UTF-8. UTF-8 is by far the common case; the one other thing
// handled is Latin-1 and its Windows cousin, which old pages still declare.
func decode(b []byte, charset string) string {
	switch strings.ToLower(charset) {
	case "iso-8859-1", "latin1", "windows-1252", "us-ascii", "iso-8859-15":
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	}
	return strings.ToValidUTF8(string(b), "\uFFFD")
}

// cutText keeps the first max bytes, at a line or word end where one is near, and says how
// much was left out.
func cutText(s string, max int) (string, int) {
	if len(s) <= max {
		return s, 0
	}
	cut := max
	for cut > max-400 && cut > 0 && s[cut] != '\n' && s[cut] != ' ' {
		cut--
	}
	if cut <= max-400 {
		cut = max
	}
	return strings.ToValidUTF8(s[:cut], ""), len(s) - cut
}

// simplify turns a transport error into the sentence the model (and user) should read,
// without the URL-and-plumbing noise net/http wraps around it.
func simplify(err error) error {
	switch {
	case errors.Is(err, errBlocked):
		return errBlocked
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("the site took longer than %s to answer", Timeout)
	case errors.Is(err, context.Canceled):
		return errors.New("the request was cancelled")
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return fmt.Errorf("the address %q does not exist", dns.Name)
	}
	return err
}
