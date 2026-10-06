package clients

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

type Target struct {
	DialAddr   string // ip:port для дозвона
	Path       string // path?query для запроса
	HostHeader string // значение заголовка Host
}

// PrepareTarget подготавливает цель для http клиента.
//   - Берет из URL только схему, хост, порт, путь и query
//   - Проверяет схему - должна быть http, может быть опущена
//   - Резолвит hostname в IPv4
//   - Чистит query от dropKeys
func PrepareTarget(baseURL string, dropKeys []string) (Target, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return Target{}, fmt.Errorf("parse url: %w", err)
	}

	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" {
		return Target{}, fmt.Errorf("scheme must be http, got %q", scheme)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return Target{}, errors.New("empty host in URL")
	}

	// Резолвим один раз и только в IPv4: при "ip", например, localhost может
	// разрезолвиться в ::1, а сервер обычно слушает 127.0.0.1.
	ipAddr, err := net.ResolveIPAddr("ip4", hostname)
	if err != nil {
		return Target{}, err
	}

	port := u.Port()
	if port == "" {
		port = "80"
	}

	var path strings.Builder
	if escPath := u.EscapedPath(); escPath == "" {
		path.WriteByte('/')
	} else {
		path.WriteString(escPath)
	}

	q := u.Query()
	for _, key := range dropKeys {
		q.Del(key)
	}
	rawQuery := q.Encode()

	if rawQuery != "" {
		path.WriteByte('?')
		path.WriteString(rawQuery)
	}

	return Target{
		DialAddr:   net.JoinHostPort(ipAddr.String(), port),
		Path:       path.String(),
		HostHeader: u.Host,
	}, nil
}
