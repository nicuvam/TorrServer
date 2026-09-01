package qbit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type capturedRequest struct {
	Method      string
	Path        string
	Query       url.Values
	Form        url.Values
	Body        []byte
	ContentType string
	Referer     string
	Origin      string
	SID         string
}

type mockQB struct {
	server *httptest.Server

	mu          sync.Mutex
	requests    []capturedRequest
	logins      int
	loginStatus int
	loginBody   string
	loginCookie string
	handler     func(w http.ResponseWriter, req capturedRequest)
}

func newMockQB(t *testing.T) *mockQB {
	t.Helper()

	mock := &mockQB{
		loginStatus: http.StatusOK,
		loginBody:   loginSuccessBody,
		loginCookie: "session-1",
	}
	mock.server = httptest.NewServer(http.HandlerFunc(mock.serve))
	t.Cleanup(mock.server.Close)
	return mock
}

func (m *mockQB) client() *Client {
	return New(m.server.URL, "user", "pass")
}

func (m *mockQB) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	req := capturedRequest{
		Method:      r.Method,
		Path:        r.URL.Path,
		Query:       r.URL.Query(),
		Body:        body,
		ContentType: r.Header.Get("Content-Type"),
		Referer:     r.Header.Get("Referer"),
		Origin:      r.Header.Get("Origin"),
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		req.SID = cookie.Value
	}
	if strings.HasPrefix(req.ContentType, formContentType) {
		req.Form, _ = url.ParseQuery(string(body))
	}

	m.mu.Lock()
	m.requests = append(m.requests, req)
	isLogin := r.URL.Path == "/"+loginPath
	if isLogin {
		m.logins++
	}
	status, loginBody, cookie := m.loginStatus, m.loginBody, m.loginCookie
	handler := m.handler
	m.mu.Unlock()

	if isLogin {
		if status >= http.StatusOK && status < http.StatusMultipleChoices && (loginBody == loginSuccessBody || loginBody == "") && cookie != "" {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: cookie})
		}
		w.WriteHeader(status)
		w.Write([]byte(loginBody))
		return
	}

	if handler != nil {
		handler(w, req)
		return
	}
	w.Write([]byte("[]"))
}

func (m *mockQB) setLogin(status int, body, cookie string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loginStatus, m.loginBody, m.loginCookie = status, body, cookie
}

func (m *mockQB) setHandler(handler func(w http.ResponseWriter, req capturedRequest)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

func (m *mockQB) loginCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.logins
}

func (m *mockQB) captured() []capturedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]capturedRequest(nil), m.requests...)
}

func (m *mockQB) lastRequest(path string) capturedRequest {
	requests := m.captured()
	for i := len(requests) - 1; i >= 0; i-- {
		if requests[i].Path == "/"+path {
			return requests[i]
		}
	}
	return capturedRequest{}
}

func (m *mockQB) countPath(path string) int {
	count := 0
	for _, req := range m.captured() {
		if req.Path == "/"+path {
			count++
		}
	}
	return count
}

func TestLoginOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		cookie  string
		wantErr error
	}{
		{name: "ok", status: http.StatusOK, body: "Ok.", cookie: "session-1"},
		{name: "no content 5.2", status: http.StatusNoContent, body: "", cookie: "session-1"},
		{name: "empty body", status: http.StatusOK, body: "", cookie: "session-1"},
		{name: "fails body", status: http.StatusOK, body: "Fails.", cookie: "session-1", wantErr: ErrAuth},
		{name: "unauthorized", status: http.StatusUnauthorized, body: "", cookie: "", wantErr: ErrAuth},
		{name: "banned", status: http.StatusForbidden, body: "", cookie: "", wantErr: ErrBanned},
		{name: "no cookie", status: http.StatusOK, body: "Ok.", cookie: "", wantErr: ErrAuth},
		{name: "server error", status: http.StatusInternalServerError, body: "", cookie: "", wantErr: ErrUnreachable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mock := newMockQB(t)
			mock.setLogin(test.status, test.body, test.cookie)
			client := mock.client()

			_, err := client.Info(nil)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("got error %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestLoginSendsCredentials(t *testing.T) {
	mock := newMockQB(t)
	client := mock.client()

	if _, err := client.Info(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	requests := mock.captured()
	login := requests[0]
	if login.Method != http.MethodPost || login.Path != "/"+loginPath {
		t.Fatalf("unexpected login request: %s %s", login.Method, login.Path)
	}
	if login.Form.Get("username") != "user" || login.Form.Get("password") != "pass" {
		t.Fatalf("unexpected login form: %v", login.Form)
	}
	if requests[1].SID != "session-1" {
		t.Fatalf("got session cookie %q, want session-1", requests[1].SID)
	}
}

func TestNotConfigured(t *testing.T) {
	client := New("", "user", "pass")

	if _, err := client.Info(nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got error %v, want %v", err, ErrNotConfigured)
	}
}

func TestRefererAndOriginOnEveryRequest(t *testing.T) {
	mock := newMockQB(t)
	client := mock.client()

	if _, err := client.Info(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := client.Delete([]string{"aabb"}, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := client.Files("aabb"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	requests := mock.captured()
	if len(requests) != 4 {
		t.Fatalf("got %d requests, want 4", len(requests))
	}
	for _, req := range requests {
		if req.Referer != mock.server.URL {
			t.Fatalf("%s: got referer %q, want %q", req.Path, req.Referer, mock.server.URL)
		}
		if req.Origin != mock.server.URL {
			t.Fatalf("%s: got origin %q, want %q", req.Path, req.Origin, mock.server.URL)
		}
	}
}

func TestSessionReuse(t *testing.T) {
	mock := newMockQB(t)
	client := mock.client()

	for i := 0; i < 5; i++ {
		if _, err := client.Info(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if mock.loginCount() != 1 {
		t.Fatalf("got %d logins, want 1", mock.loginCount())
	}
	if mock.countPath(infoPath) != 5 {
		t.Fatalf("got %d info requests, want 5", mock.countPath(infoPath))
	}
}

func TestReloginOnceOnForbidden(t *testing.T) {
	mock := newMockQB(t)
	var forbidden atomic.Bool
	forbidden.Store(true)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		if forbidden.Swap(false) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte("[]"))
	})
	client := mock.client()

	if _, err := client.Info(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.loginCount() != 2 {
		t.Fatalf("got %d logins, want 2", mock.loginCount())
	}
	if mock.countPath(infoPath) != 2 {
		t.Fatalf("got %d info requests, want 2", mock.countPath(infoPath))
	}
}

func TestNoThirdLoginOnDoubleForbidden(t *testing.T) {
	mock := newMockQB(t)
	mock.setHandler(func(w http.ResponseWriter, req capturedRequest) {
		w.WriteHeader(http.StatusForbidden)
	})
	client := mock.client()

	if _, err := client.Info(nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("got error %v, want %v", err, ErrAuth)
	}
	if mock.loginCount() != 2 {
		t.Fatalf("got %d logins, want 2", mock.loginCount())
	}
	if mock.countPath(infoPath) != 2 {
		t.Fatalf("got %d info requests, want 2", mock.countPath(infoPath))
	}
}

func TestAuthCooldownBlocksLogin(t *testing.T) {
	mock := newMockQB(t)
	mock.setLogin(http.StatusOK, "Fails.", "")
	client := mock.client()

	clock := time.Now()
	client.now = func() time.Time { return clock }

	if _, err := client.Info(nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("got error %v, want %v", err, ErrAuth)
	}
	if _, err := client.Info(nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("got error %v, want %v", err, ErrAuth)
	}
	if mock.loginCount() != 1 {
		t.Fatalf("got %d logins during cooldown, want 1", mock.loginCount())
	}

	clock = clock.Add(authCooldown + time.Second)
	if _, err := client.Info(nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("got error %v, want %v", err, ErrAuth)
	}
	if mock.loginCount() != 2 {
		t.Fatalf("got %d logins after cooldown, want 2", mock.loginCount())
	}
}

func TestBanCooldownBlocksLogin(t *testing.T) {
	mock := newMockQB(t)
	mock.setLogin(http.StatusForbidden, "", "")
	client := mock.client()

	clock := time.Now()
	client.now = func() time.Time { return clock }

	if _, err := client.Info(nil); !errors.Is(err, ErrBanned) {
		t.Fatalf("got error %v, want %v", err, ErrBanned)
	}

	clock = clock.Add(authCooldown + time.Second)
	if _, err := client.Info(nil); !errors.Is(err, ErrBanned) {
		t.Fatalf("got error %v, want %v", err, ErrBanned)
	}
	if mock.loginCount() != 1 {
		t.Fatalf("got %d logins during ban cooldown, want 1", mock.loginCount())
	}

	clock = clock.Add(banCooldown)
	if _, err := client.Info(nil); !errors.Is(err, ErrBanned) {
		t.Fatalf("got error %v, want %v", err, ErrBanned)
	}
	if mock.loginCount() != 2 {
		t.Fatalf("got %d logins after ban cooldown, want 2", mock.loginCount())
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "", want: ""},
		{raw: "  http://host:8080/  ", want: "http://host:8080"},
		{raw: "host:8080", want: "http://host:8080"},
		{raw: "https://host/qb//", want: "https://host/qb"},
	}

	for _, test := range tests {
		if got := normalizeBaseURL(test.raw); got != test.want {
			t.Fatalf("normalizeBaseURL(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestModernSessionCookieName(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+loginPath {
			http.SetCookie(w, &http.Cookie{Name: "QBT_SID_8080", Value: "modern-sid"})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if c, err := r.Cookie("QBT_SID_8080"); err == nil {
			gotCookie = c.Value
		}
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client := New(srv.URL, "user", "pass")
	if _, err := client.Info(nil); err != nil {
		t.Fatalf("Info: %v", err)
	}
	if gotCookie != "modern-sid" {
		t.Fatalf("expected QBT_SID_8080 cookie on request, got %q", gotCookie)
	}
}
