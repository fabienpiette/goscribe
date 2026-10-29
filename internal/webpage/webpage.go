// Package webpage fetches a public web page as Markdown and detects login walls.
package webpage

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
	"sync/atomic"
	"time"
	"unicode/utf8"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

const (
	maxBody      = 5 << 20
	maxRedirects = 5
	minTextLen   = 200
)

// Page is a fetched page. URL is the final one after redirects.
type Page struct {
	Title, URL, Markdown string
}

// LoginError means the page needs an authenticated session.
type LoginError struct{ Reason string }

func (e LoginError) Error() string { return "needs a login: " + e.Reason }

// ErrNoText means the page has nothing readable once converted.
var ErrNoText = errors.New("the page has no readable text (it may need JavaScript or a login)")

var errPrivate = errors.New("redirected to a private address")

// lookupIP resolves host names; tests swap it.
var lookupIP = net.DefaultResolver.LookupIPAddr

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

var (
	nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")
)

func defaultIsPrivate(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return addrPrivate(addr.Unmap())
}

func addrPrivate(a netip.Addr) bool {
	a = a.Unmap()
	if a.Is6() {
		b := a.As16()
		switch {
		case nat64Prefix.Contains(a):
			return addrPrivate(netip.AddrFrom4([4]byte(b[12:16])))
		case sixToFour.Contains(a):
			return addrPrivate(netip.AddrFrom4([4]byte(b[2:6])))
		}
	}
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// isPrivate reports addresses a redirect from a public page must not reach.
// It is a variable so tests can choose which addresses count as private.
var isPrivate = defaultIsPrivate

type guardKey struct{}

// guard is stored in the request context. Once a redirect was followed, a
// private address is refused unless the origin itself was private.
type guard struct {
	originPrivate atomic.Bool
	redirected    atomic.Bool
	proxied       atomic.Bool
}

func resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	addrs, err := lookupIP(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no address for %s", host)
	}
	return ips, nil
}

func anyPrivate(ips []net.IP) bool {
	for _, ip := range ips {
		if isPrivate(ip) {
			return true
		}
	}
	return false
}

func guardedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	g, _ := ctx.Value(guardKey{}).(*guard)
	if g == nil || g.proxied.Load() {
		return d.DialContext(ctx, network, addr) // the proxy's address is not checked
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	redirected := g.redirected.Load()
	if redirected && !g.originPrivate.Load() && anyPrivate(ips) {
		return nil, errPrivate
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err != nil {
			lastErr = err
			continue
		}
		if !redirected {
			g.originPrivate.Store(isPrivate(ip))
		}
		return conn, nil
	}
	return nil, lastErr
}

var transport = newTransport()

func newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = guardedDial
	t.DisableKeepAlives = true // fetches are one-shot, so no pooled connection skips the guard
	return t
}

func usesProxy(req *http.Request) bool {
	if transport.Proxy == nil {
		return false
	}
	u, err := transport.Proxy(req)
	return err == nil && u != nil
}

// client is swapped out in tests.
//
// Known residual risk: through a public proxy, DNS rebinding of a redirect
// target between CheckRedirect and the proxy's own lookup is not caught.
var client = &http.Client{
	Timeout:   20 * time.Second,
	Transport: transport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errors.New("stopped after 5 redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("redirected to unsupported scheme %q", req.URL.Scheme)
		}
		g, _ := req.Context().Value(guardKey{}).(*guard)
		if g == nil {
			return nil
		}
		g.redirected.Store(true)
		g.proxied.Store(usesProxy(req))
		if !g.originPrivate.Load() {
			ips, err := resolve(req.Context(), req.URL.Hostname())
			if err != nil {
				return err
			}
			if anyPrivate(ips) {
				return errPrivate
			}
		}
		return nil
	},
}

// LoginHost reports whether host is a service that is known to need a login.
func LoginHost(host string) bool {
	h := strings.ToLower(host)
	switch h {
	case "docs.google.com", "drive.google.com", "accounts.google.com", "notion.so", "www.notion.so",
		"linear.app", "app.slack.com", "figma.com", "www.figma.com", "login.microsoftonline.com":
		return true
	}
	for _, suffix := range []string{".notion.site", ".atlassian.net", ".sharepoint.com"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

func signInPath(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		switch strings.ToLower(seg) {
		case "login", "signin", "sign-in", "sso", "auth":
			return true
		}
	}
	return false
}

// Fetch downloads rawURL and returns it as Markdown.
func Fetch(ctx context.Context, rawURL string) (Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Page{}, fmt.Errorf("invalid URL %q", rawURL)
	}
	if LoginHost(u.Hostname()) {
		return Page{}, &LoginError{Reason: u.Hostname() + " needs a login"}
	}
	g := &guard{}
	ctx = context.WithValue(ctx, guardKey{}, g)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	if usesProxy(req) {
		// The first connection goes to the proxy, so judge the typed host itself.
		g.proxied.Store(true)
		h := u.Hostname()
		ip := net.ParseIP(h)
		g.originPrivate.Store(strings.EqualFold(h, "localhost") || (ip != nil && isPrivate(ip)))
	}
	req.Header.Set("User-Agent", "goscribe")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errPrivate) {
			return Page{}, errPrivate
		}
		return Page{}, err
	}
	defer resp.Body.Close()

	final := resp.Request.URL
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Page{}, &LoginError{Reason: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Page{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if LoginHost(final.Hostname()) || signInPath(final.Path) {
		return Page{}, &LoginError{Reason: "redirected to a sign-in page"}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Page{}, err
	}
	if len(body) > maxBody {
		return Page{}, errors.New("the page is over 5 MB")
	}

	ctype, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	var title, md string
	switch ctype {
	case "text/html", "application/xhtml+xml":
		doc, perr := html.Parse(strings.NewReader(string(body)))
		if perr != nil {
			return Page{}, perr
		}
		if hasPasswordInput(doc) {
			return Page{}, &LoginError{Reason: "the page asks for a password"}
		}
		md, err = htmltomarkdown.ConvertString(string(body))
		if err != nil {
			return Page{}, err
		}
		title = htmlTitle(doc)
		if title == "" {
			title = firstHeading(md)
		}
		if title == "" {
			title = final.Host
		}
	case "text/markdown", "text/plain":
		md = string(body)
		title = firstHeading(md)
		if title == "" {
			segs := strings.Split(strings.Trim(final.Path, "/"), "/")
			title = segs[len(segs)-1]
			if title == "" {
				title = final.Host
			}
		}
	default:
		return Page{}, fmt.Errorf("unsupported content type %s", ctype)
	}

	if utf8.RuneCountInString(strings.TrimSpace(md)) < minTextLen {
		return Page{}, ErrNoText
	}
	return Page{Title: title, URL: final.String(), Markdown: md}, nil
}

func hasPasswordInput(n *html.Node) bool {
	if n.Type == html.ElementNode && n.Data == "input" {
		for _, a := range n.Attr {
			if a.Key == "type" && strings.EqualFold(strings.TrimSpace(a.Val), "password") {
				return true
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasPasswordInput(c) {
			return true
		}
	}
	return false
}

func htmlTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		var sb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				sb.WriteString(c.Data)
			}
		}
		return strings.Join(strings.Fields(sb.String()), " ")
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := htmlTitle(c); t != "" {
			return t
		}
	}
	return ""
}

func firstHeading(md string) string {
	for _, line := range strings.Split(md, "\n") {
		if rest, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
