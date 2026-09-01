package qbit

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout = 15 * time.Second
	authCooldown   = time.Minute
	banCooldown    = 15 * time.Minute
	sessionCookie  = "SID"

	sessionCookiePrefix = "QBT_SID_"
)

type Client struct {
	baseURL  string
	username string
	password string

	http *http.Client
	now  func() time.Time

	mu             sync.Mutex
	sid            string
	sidName        string
	cookieless     bool
	apiVersion     string
	loginBlockedTo time.Time
	loginBlockErr  error
}

func New(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  normalizeBaseURL(baseURL),
		username: username,
		password: password,
		http:     &http.Client{Timeout: requestTimeout},
		now:      time.Now,
	}
}

func (c *Client) cookieName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sidName != "" {
		return c.sidName
	}
	return sessionCookie
}

type response struct {
	status  int
	body    []byte
	cookies []*http.Cookie
}

func (c *Client) do(method, path, contentType string, body []byte) ([]byte, error) {
	if c.baseURL == "" {
		return nil, ErrNotConfigured
	}

	sid, err := c.session()
	if err != nil {
		return nil, err
	}

	resp, err := c.send(method, path, contentType, body, sid)
	if err != nil {
		return nil, err
	}

	if resp.status == http.StatusForbidden {
		sid, err = c.relogin(sid)
		if err != nil {
			return nil, err
		}
		resp, err = c.send(method, path, contentType, body, sid)
		if err != nil {
			return nil, err
		}
	}

	return checkStatus(resp)
}

func (c *Client) send(method, path, contentType string, body []byte, sid string) (*response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, c.baseURL+"/"+strings.TrimLeft(path, "/"), reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Referer", c.baseURL)
	req.Header.Set("Origin", c.baseURL)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: c.cookieName(), Value: sid})
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	return &response{status: resp.StatusCode, body: data, cookies: resp.Cookies()}, nil
}

func checkStatus(resp *response) ([]byte, error) {
	switch {
	case resp.status == http.StatusOK:
		return resp.body, nil
	case resp.status == http.StatusUnauthorized, resp.status == http.StatusForbidden:
		return nil, ErrAuth
	case resp.status == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.status == http.StatusUnsupportedMediaType:
		return nil, ErrBadTorrent
	case resp.status >= http.StatusInternalServerError:
		return nil, fmt.Errorf("%w: %s", ErrUnreachable, http.StatusText(resp.status))
	}
	return nil, &APIError{Status: resp.status, Body: strings.TrimSpace(string(resp.body))}
}

func withQuery(path string, params url.Values) string {
	if len(params) == 0 {
		return path
	}
	return path + "?" + params.Encode()
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}
