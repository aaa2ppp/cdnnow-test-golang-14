package clients

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

type Config struct {
	BaseURL     string
	DialAddr    string
	UserInfo    *url.Userinfo
	Timeout     time.Duration
	NoKeepAlive bool
}

// Single одно соединение к серверу, не является потокобезопасным
type Single struct {
	client  *fasthttp.HostClient
	req     *fasthttp.Request
	resp    *fasthttp.Response
	uriBuf  []byte
	timeout time.Duration
}

func NewSingle(c Config) *Single {
	client := &fasthttp.HostClient{
		Addr:     c.DialAddr,
		MaxConns: 1,

		MaxIdemponentCallAttempts: 1,
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("POST")
	if c.NoKeepAlive {
		req.Header.SetConnectionClose()
	}
	if c.UserInfo != nil {
		pass, _ := c.UserInfo.Password()
		setBasicAuth(req, c.UserInfo.Username(), pass)
	}

	uriBuf := make([]byte, 0, len(c.BaseURL)+32)
	uriBuf = append(uriBuf, c.BaseURL...)
	if strings.ContainsRune(c.BaseURL, '?') {
		uriBuf = append(uriBuf, "&num="...)
	} else {
		uriBuf = append(uriBuf, "?num="...)
	}

	// преалацируем память в req под URI
	maxURI := strconv.AppendInt(uriBuf, math.MinInt64, 10)
	req.SetRequestURIBytes(maxURI)

	return &Single{
		client:  client,
		req:     req,
		resp:    fasthttp.AcquireResponse(),
		uriBuf:  uriBuf,
		timeout: c.Timeout,
	}
}

func setBasicAuth(req *fasthttp.Request, username, password string) {
	auth := username + ":" + password
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	req.Header.Set("Authorization", "Basic "+encoded)
}

func isOK(status int) bool {
	return status/100*100 == 200
}

func errorMsg(code int, body []byte) (msg string) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return http.StatusText(code)
	}
	if len(body) > 256 {
		msg = string(body[:253]) + "..."
	}
	return string(body)
}

func (c *Single) do() (time.Duration, error) {

	// Мы хотим знать время ответа сервера, но latency - это server time + client time.
	// Mаксимально сокращаем долю клиента.
	start := time.Now()
	err := c.client.DoTimeout(c.req, c.resp, c.timeout)
	if err != nil {
		return 0, err
	}
	latency := time.Since(start)

	if code := c.resp.StatusCode(); !isOK(code) {
		msg := errorMsg(code, c.resp.Body())
		return 0, &HTTPError{Code: code, Message: msg} // копирование msg
	}

	return latency, nil
}

func (c *Single) DoRequest(num int) (time.Duration, error) {
	uri := strconv.AppendInt(c.uriBuf, int64(num), 10)
	c.req.SetRequestURIBytes(uri)
	return c.do()
}

// Probe шлет ?num=ABC - сервер должен ответить 400
func (c *Single) Probe() error {
	uri := append(c.uriBuf, "ABC"...)
	c.req.SetRequestURIBytes(uri)

	if err := c.client.DoTimeout(c.req, c.resp, c.timeout); err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	if code := c.resp.StatusCode(); code != 400 {
		msg := errorMsg(code, c.resp.Body())
		return fmt.Errorf("probe: unexpected status %d %q, want 400", code, msg)
	}
	return nil
}
