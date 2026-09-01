package qbit

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	versionPath = "api/v2/app/webapiVersion"

	stopPath   = "api/v2/torrents/stop"
	startPath  = "api/v2/torrents/start"
	pausePath  = "api/v2/torrents/pause"
	resumePath = "api/v2/torrents/resume"

	stopStartMajor = 2
	stopStartMinor = 11
)

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

func (c *Client) StopPath() (string, error) {
	version, err := c.APIVersion()
	if err != nil {
		return "", err
	}
	if atLeastVersion(version, stopStartMajor, stopStartMinor) {
		return stopPath, nil
	}
	return pausePath, nil
}

func (c *Client) StartPath() (string, error) {
	version, err := c.APIVersion()
	if err != nil {
		return "", err
	}
	if atLeastVersion(version, stopStartMajor, stopStartMinor) {
		return startPath, nil
	}
	return resumePath, nil
}

func atLeastVersion(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	gotMajor := leadingNumber(parts[0])
	gotMinor := 0
	if len(parts) > 1 {
		gotMinor = leadingNumber(parts[1])
	}
	if gotMajor != major {
		return gotMajor > major
	}
	return gotMinor >= minor
}

func leadingNumber(part string) int {
	end := 0
	for end < len(part) && part[end] >= '0' && part[end] <= '9' {
		end++
	}
	number, _ := strconv.Atoi(part[:end])
	return number
}
