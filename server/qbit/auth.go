package qbit

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"server/log"
)

const (
	loginPath        = "api/v2/auth/login"
	loginSuccessBody = "Ok."
)

func (c *Client) session() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sid != "" {
		return c.sid, nil
	}
	return c.loginLocked()
}

func (c *Client) relogin(stale string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sid != "" && c.sid != stale {
		return c.sid, nil
	}
	c.sid = ""
	return c.loginLocked()
}

func (c *Client) loginLocked() (string, error) {
	if c.loginBlockErr != nil {
		if c.now().Before(c.loginBlockedTo) {
			return "", c.loginBlockErr
		}
		c.loginBlockErr = nil
	}

	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)

	resp, err := c.send(http.MethodPost, loginPath, formContentType, []byte(form.Encode()), "")
	if err != nil {
		return "", err
	}

	switch {
	case resp.status == http.StatusForbidden:
		return "", c.blockLogin(banCooldown, ErrBanned)
	case resp.status == http.StatusUnauthorized:
		return "", c.blockLogin(authCooldown, ErrAuth)
	case resp.status >= http.StatusInternalServerError:
		return "", fmt.Errorf("%w: %s", ErrUnreachable, http.StatusText(resp.status))
	case resp.status < http.StatusOK || resp.status >= http.StatusMultipleChoices:
		return "", &APIError{Status: resp.status, Body: strings.TrimSpace(string(resp.body))}
	}

	if body := strings.TrimSpace(string(resp.body)); body != "" && body != loginSuccessBody {
		return "", c.blockLogin(authCooldown, ErrAuth)
	}

	name, sid := sessionID(resp.cookies)
	if sid == "" {
		return "", c.blockLogin(authCooldown, fmt.Errorf("%w: no session cookie", ErrAuth))
	}

	c.sid = sid
	c.sidName = name
	return sid, nil
}

func (c *Client) blockLogin(cooldown time.Duration, err error) error {
	c.sid = ""
	c.loginBlockedTo = c.now().Add(cooldown)
	c.loginBlockErr = err
	log.TLogln("qBittorrent login failed:", c.baseURL, err)
	return err
}

func sessionID(cookies []*http.Cookie) (string, string) {
	for _, cookie := range cookies {
		if cookie.Value == "" {
			continue
		}
		if cookie.Name == sessionCookie || strings.HasPrefix(cookie.Name, sessionCookiePrefix) {
			return cookie.Name, cookie.Value
		}
	}
	return "", ""
}
