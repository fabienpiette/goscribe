// Package webpage fetches a public web page as Markdown and detects login walls.
package webpage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

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

// client is swapped out in tests.
var client = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errors.New("stopped after 5 redirects")
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", "goscribe")
	resp, err := client.Do(req)
	if err != nil {
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
		}
	default:
		return Page{}, fmt.Errorf("unsupported content type %s", ctype)
	}

	if len(strings.TrimSpace(md)) < minTextLen {
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
