//go:build test

package calculators

func (c *Async) IgnoreOverload() {
	c.ignoreOverload = true
}

func (c *Parallel) IgnoreOverload() {
	c.sum.ignoreOverload = true
	c.sub.ignoreOverload = true
}
