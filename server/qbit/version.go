package qbit

import (
	"fmt"
	"net/http"
	"strings"
)

const versionPath = "api/v2/app/webapiVersion"

func (c *Client) APIVersion() (string, error) {
	c.mu.Lock()
	version := c.apiVersion
	c.mu.Unlock()
	if version != "" {
		return version, nil
	}

	data, err := c.do(http.MethodGet, versionPath, "", nil)
	if err != nil {
		return "", err
	}

	version = strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("qbittorrent: empty web api version")
	}

	c.mu.Lock()
	c.apiVersion = version
	c.mu.Unlock()
	return version, nil
}
