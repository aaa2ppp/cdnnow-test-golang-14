package clients

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

type Target struct {
	BaseURL  string        // для req.SetRequestURI: scheme://host/path?query
	DialAddr string        // ip:port для Dialer
	UserInfo *url.Userinfo // Basic auth, если был user:pass@
}

func ParseTarget(baseURL string, dropKeys []string) (Target, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return Target{}, fmt.Errorf("parse url: %w", err)
	}

	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return Target{}, fmt.Errorf("scheme must be http or https, got %q", scheme)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return Target{}, errors.New("empty host in URL")
	}

	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// чистим query
	q := u.Query()
	for _, key := range dropKeys {
		q.Del(key)
	}
	rawQuery := q.Encode()

	// Собираем обратно ручками
	var sb strings.Builder
	sb.Grow(len(baseURL) + 8)
	sb.WriteString(scheme)
	sb.WriteString("://")
	sb.WriteString(u.Host) // hostname[:port] как ввел пользователь
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	sb.WriteString(path)
	if rawQuery != "" {
		sb.WriteByte('?')
		sb.WriteString(rawQuery)
	}

	return Target{
		BaseURL:  sb.String(),
		DialAddr: net.JoinHostPort(hostname, port),
		UserInfo: u.User,
	}, nil
}
