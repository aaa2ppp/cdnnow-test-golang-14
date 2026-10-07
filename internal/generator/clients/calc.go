package clients

import (
	"bytes"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

type Config struct {
	Target      Target
	Timeout     time.Duration
	NoKeepAlive bool
}

const calcNumKey = "num"

func CalcKeys() []string {
	return []string{calcNumKey}
}

// Calc клиент. Создает одно соединение к серверу, не является потокобезопасным
type Calc struct {
	client  *fasthttp.HostClient
	req     *fasthttp.Request
	resp    *fasthttp.Response
	uriBuf  []byte
	timeout time.Duration
}

func NewCalc(c Config) *Calc {
	t := c.Target
	client := &fasthttp.HostClient{
		Addr:     t.DialAddr,
		MaxConns: 1,

		MaxIdemponentCallAttempts: 1,
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("POST")
	if c.NoKeepAlive {
		req.Header.SetConnectionClose()
	}
	req.Header.SetHost(t.HostHeader)

	const maxInt64Digits = 20 // len("-9223372036854775808")
	uriBuf := make([]byte, 0, len(t.Path)+len(calcNumKey)+2+maxInt64Digits)
	uriBuf = append(uriBuf, t.Path...)
	if strings.ContainsRune(t.Path, '?') {
		uriBuf = append(uriBuf, '&')
	} else {
		uriBuf = append(uriBuf, '?')
	}
	uriBuf = append(uriBuf, calcNumKey...)
	uriBuf = append(uriBuf, '=')

	// преалацируем память в req под URI
	maxURI := strconv.AppendInt(uriBuf, math.MinInt64, 10)
	req.SetRequestURIBytes(maxURI)

	return &Calc{
		client:  client,
		req:     req,
		resp:    fasthttp.AcquireResponse(),
		uriBuf:  uriBuf,
		timeout: c.Timeout,
	}
}

func isOK(status int) bool {
	return status/100*100 == 200
}

func errorMsg(code int, body []byte) (msg string) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return http.StatusText(code)
	}
	if len(body) > 255 {
		return string(body[:252]) + "..."
	}
	return string(body)
}

func (c *Calc) DoRequest(num int) (time.Duration, error) {
	uri := strconv.AppendInt(c.uriBuf, int64(num), 10)
	c.req.SetRequestURIBytes(uri)

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
		return latency, &HTTPError{Code: code, Message: msg}
	}

	return latency, nil
}
