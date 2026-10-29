package webpage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("unexpected request to %s", r.URL)
	return nil, errors.New("no network in this test")
}

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func long() string { return strings.Repeat("Acme builds reliable widgets for everyone. ", 10) }

func TestFetchHTML(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><title>
  Acme   Guide </title></head><body><h1>Welcome</h1><p>%s</p><script>var secret = "hidden";</script></body></html>`, long())
	})
	p, err := Fetch(context.Background(), srv.URL+"/guide")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Acme Guide" {
		t.Errorf("title = %q", p.Title)
	}
	if !strings.Contains(p.Markdown, "# Welcome") {
		t.Errorf("markdown missing heading: %q", p.Markdown)
	}
	if strings.Contains(p.Markdown, "secret") {
		t.Errorf("script text leaked: %q", p.Markdown)
	}
	if p.URL != srv.URL+"/guide" {
		t.Errorf("url = %q", p.URL)
	}
}

func TestFetchPlainMarkdown(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		fmt.Fprintf(w, "# Notes\n\n%s", long())
	})
	p, err := Fetch(context.Background(), srv.URL+"/n.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Notes" || !strings.HasPrefix(p.Markdown, "# Notes") {
		t.Errorf("got %+v", p)
	}
}

func TestFetch401403(t *testing.T) {
	for _, code := range []int{401, 403} {
		srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		_, err := Fetch(context.Background(), srv.URL)
		var le *LoginError
		if !errors.As(err, &le) || le.Reason != fmt.Sprintf("HTTP %d", code) {
			t.Errorf("code %d: err = %v", code, err)
		}
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	_, err := Fetch(context.Background(), srv.URL)
	var le *LoginError
	if err == nil || errors.As(err, &le) || err.Error() != "HTTP 500" {
		t.Errorf("500: err = %v", err)
	}
}

func TestFetchRedirectToSignIn(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Login" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<p>%s</p>", long())
			return
		}
		http.Redirect(w, r, "/Login", http.StatusFound)
	})
	_, err := Fetch(context.Background(), srv.URL+"/private")
	var le *LoginError
	if !errors.As(err, &le) || le.Reason != "redirected to a sign-in page" {
		t.Errorf("err = %v", err)
	}
}

func TestFetchPasswordForm(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><p>%s</p><form><input type="PASSWORD" name="p"></form></body></html>`, long())
	})
	_, err := Fetch(context.Background(), srv.URL)
	var le *LoginError
	if !errors.As(err, &le) || le.Reason != "the page asks for a password" {
		t.Errorf("err = %v", err)
	}
}

func TestFetchJSShell(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>App</title></head><body><div id=root></div></body></html>`)
	})
	_, err := Fetch(context.Background(), srv.URL)
	if !errors.Is(err, ErrNoText) {
		t.Errorf("err = %v", err)
	}
}

func TestFetchTooBig(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("a", maxBody+10)))
	})
	_, err := Fetch(context.Background(), srv.URL)
	if err == nil || err.Error() != "the page is over 5 MB" {
		t.Errorf("err = %v", err)
	}
}

func TestFetchUnsupportedType(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF"))
	})
	_, err := Fetch(context.Background(), srv.URL)
	if err == nil || err.Error() != "unsupported content type application/pdf" {
		t.Errorf("err = %v", err)
	}
}

func TestFetchLoginHostNoRequest(t *testing.T) {
	old := client
	client = &http.Client{Transport: failTransport{t}}
	t.Cleanup(func() { client = old })
	_, err := Fetch(context.Background(), "https://docs.google.com/document/d/abc")
	var le *LoginError
	if !errors.As(err, &le) || le.Reason != "docs.google.com needs a login" {
		t.Errorf("err = %v", err)
	}
	if err.Error() != "needs a login: docs.google.com needs a login" {
		t.Errorf("message = %q", err.Error())
	}
}

func TestLoginHost(t *testing.T) {
	yes := []string{"docs.google.com", "drive.google.com", "accounts.google.com", "notion.so", "www.notion.so",
		"acme.notion.site", "linear.app", "acme.atlassian.net", "app.slack.com", "figma.com", "www.figma.com",
		"login.microsoftonline.com", "globex.sharepoint.com", "DOCS.Google.com"}
	no := []string{"example.com", "google.com", "notion.site", "evilnotion.so", "atlassian.net.example.com"}
	for _, h := range yes {
		if !LoginHost(h) {
			t.Errorf("LoginHost(%q) = false", h)
		}
	}
	for _, h := range no {
		if LoginHost(h) {
			t.Errorf("LoginHost(%q) = true", h)
		}
	}
}
