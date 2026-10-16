//go:build test

package pipeline

func (p *Pipeline) IgnoreOverload() {
	p.ignoreOver = true
}
