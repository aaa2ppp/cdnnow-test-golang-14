package main

import (
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/generator/clients"
)

// ProbeTarget делает один "нейтральный" запрос, чтобы убедиться,
// что эндпоинт жив и отвечает корректно.
func ProbeTarget(target clients.Target, timeout time.Duration) error {
	c := clients.NewCalc(clients.Config{
		Target:      target,
		Timeout:     timeout,
		NoKeepAlive: true,
	})

	// Хак: 0 - нейтральный элемент в вычислениях.
	// Сервер проведет расчет, но результат гарантированно не изменит
	_, err := c.DoRequest(0)
	return err
}
