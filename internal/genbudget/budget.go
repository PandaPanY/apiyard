// Package genbudget bounds work shared by Schema and Mock.js generators.
package genbudget

import (
	"context"
	"errors"
)

// Limits bound each request's work, allocation, and recursion.
const (
	MaxNodes  = 10000
	MaxLength = 10000
	MaxBytes  = 2 << 20
	MaxDepth  = 40
)

// ErrLimit is returned when generation exceeds a resource budget.
var ErrLimit = errors.New("mock generation budget exceeded")
var slots = make(chan struct{}, 8)

// Acquire reserves a shared generation slot without queuing more work.
func Acquire(ctx context.Context) (func(), bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return nil, false
	}
}

// Budget tracks resources used by one generation, including nested generators.
type Budget struct {
	ctx          context.Context
	nodes, bytes int
	err          error
}

// New creates a budget tied to the request's cancellation.
func New(ctx context.Context) *Budget { return &Budget{ctx: ctx} }

// Err returns the first limit or cancellation error.
func (b *Budget) Err() error {
	if b.err == nil {
		b.err = b.ctx.Err()
	}
	return b.err
}

// Step consumes a node and checks recursion depth and cancellation.
func (b *Budget) Step(depth int) bool {
	b.nodes++
	if depth > MaxDepth || b.nodes > MaxNodes {
		b.Fail()
	}
	return b.Err() == nil
}

// Fail records a resource-limit violation.
func (b *Budget) Fail() {
	if b.err == nil {
		b.err = ErrLimit
	}
}

// Size consumes bytes and checks cancellation before more allocation.
func (b *Budget) Size(n int) bool {
	if n < 0 || n > MaxBytes-b.bytes {
		b.Fail()
	} else {
		b.bytes += n
	}
	return b.Err() == nil
}
