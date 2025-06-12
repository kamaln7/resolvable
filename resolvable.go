package resolvable

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// V is a resolvable value.
type V[T any] func() (T, error)

// Ctx is a resolvable that accepts a context
type Ctx[T any] func(ctx context.Context) (T, error)

// WithContext binds a context to the resolvable.
func (v Ctx[T]) WithContext(ctx context.Context) V[T] {
	return func() (T, error) {
		return v(ctx)
	}
}

// WithBackgroundContext binds a background context to the resolvable.
func (v Ctx[T]) WithBackgroundContext() V[T] {
	return func() (T, error) {
		return v(context.Background())
	}
}

type options struct {
	once      bool
	retry     bool
	graceful  bool
	expiry    time.Duration
	now       func() time.Time
	safe      bool
	retryOpts RetryOpts
}

type Option func(*options)

// WithOnce marks the value as resolved once and then returns the value forever.
func WithOnce() Option {
	return func(o *options) {
		o.once = true
	}
}

// WithRetry marks the value as retryable on error.
// Retry will attempt to resolve again if the value was previously resolved with an error.
func WithRetry() Option {
	return func(o *options) {
		o.retry = true
	}
}

// WithRetryOpts sets additional retry options.
func WithRetryOpts(opts RetryOpts) Option {
	return func(o *options) {
		o.retryOpts = opts
	}
}

// WithGraceful allows for graceful degradation.
// If the resolvable returns an error, the last known good value is returned alongside the new error.
func WithGraceful() Option {
	return func(o *options) {
		o.graceful = true
	}
}

// WithCacheTTL sets a cache TTL for the resolvable.
//
// This is mutually exclusive with WithOnce().
// If WithRetry() is also set, only successful values are cached.
func WithCacheTTL(ttl time.Duration) Option {
	return func(o *options) {
		o.expiry = ttl
	}
}

// WithNow sets a custom time.Now function.
func WithNow(now func() time.Time) Option {
	return func(o *options) {
		o.now = now
	}
}

// WithUnsafe prevents concurrent access to the resolvable value.
func WithUnsafe() Option {
	return func(o *options) {
		o.safe = false
	}
}

// WithSafe allows concurrent access to the resolvable value via a mutex.
func WithSafe() Option {
	return func(o *options) {
		o.safe = true
	}
}

// New creates a new resolvable value.
//
// Default options are: WithSafe().
func New[T any](fn Ctx[T], opts ...Option) Ctx[T] {
	o := options{
		safe: true,
	}
	for _, opt := range opts {
		opt(&o)
	}

	var v Ctx[T] = fn

	if o.graceful {
		v = Graceful(v)
	}

	if o.expiry > 0 {
		v = Cache(v, CacheOpts{
			Expiry: o.expiry,
			Retry:  o.retry,
			Now:    o.now,
		})
	} else if o.retry {
		v = Retry(v, o.retryOpts)
	} else if o.once {
		v = Once(v)
	}

	// safe concurrent access must go last
	if o.safe {
		v = Safe(v)
	}

	return v
}

// Graceful allows for graceful degradation.
// If the resolvable returns an error, the last known good value is returned alongside the new error.
func Graceful[T any](resolvable Ctx[T]) Ctx[T] {
	var lastGood *T
	return func(ctx context.Context) (T, error) {
		var err error
		v, err := resolvable(ctx)
		if err != nil && lastGood != nil {
			// return the last known good value with the current error
			return *lastGood, err
		}
		// persist the new value
		lastGood = &v
		return v, err
	}
}

// Once will resolve the value once and then return the value forever regardless of errors.
func Once[T any](resolvable Ctx[T]) Ctx[T] {
	return Cache(resolvable, CacheOpts{})
}

type RetryOpts struct {
	Backoff  BackOff
	MaxTries int
}

// Retry will attempt to resolve the value until it succeeds, and then it is cached forever.
func Retry[T any](resolvable Ctx[T], opts RetryOpts) Ctx[T] {
	return Cache(resolvable, CacheOpts{
		Retry:     true,
		RetryOpts: opts,
	})
}

type CacheOpts struct {
	// Expiry is the duration after which the value is considered expired.
	Expiry time.Duration
	// Retry indicates whether to retry the resolvable if it returns an error.
	Retry bool
	// Now sets a custom time.Now function.
	Now func() time.Time
}

func (o *CacheOpts) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Cache is a wrapper around a resolvable value that allows for expiry.
func Cache[T any](resolvable Ctx[T], opts CacheOpts) Ctx[T] {
	e := &cache[T]{resolvable: resolvable, CacheOpts: opts}
	return e.Resolve
}

type cache[T any] struct {
	CacheOpts
	resolvable  Ctx[T]
	value       *T
	err         error
	nextResolve time.Time
}

func (e *cache[T]) Resolve(ctx context.Context) (T, error) {
	// Expiry value of 0 means cache forever.
	// value != nil means we have resolved the value at least once.
	fmt.Printf("e.value != nil: %t, e.Expiry: %v, e.now().Before: %v\n", e.value != nil, e.Expiry, e.now().Before(e.nextResolve))
	if e.value != nil && (e.now().Before(e.nextResolve)) {
		return *e.value, e.err
	} else if e.value == nil {
		e.backoff().Reset()
	}

	v, err := e.resolvable(ctx)
	e.value = &v
	e.err = err

	next := e.Expiry
	if err == nil {
		// reset the backoff policy if the value was resolved successfully
		e.backoff().Reset()
	} else if e.Retry {
		// use the backoff policy to determine the next retry time
		next = e.backoff().NextBackOff()
	}
	e.nextResolve = e.now().Add(next)

	return v, err
}

func (e *cache[T]) backoff() BackOff {
	if e.RetryOpts.Backoff == nil {
		return &zeroBackoff{}
	}

	return e.RetryOpts.Backoff
}

// Safe guards a resolvable with a mutex.
func Safe[T any](resolvable Ctx[T]) Ctx[T] {
	var mu sync.Mutex
	return func(ctx context.Context) (T, error) {
		mu.Lock()
		defer mu.Unlock()
		return resolvable(ctx)
	}
}

// Static returns a resolvable value that always returns the same value.
func Static[T any](value T) Ctx[T] {
	return func(ctx context.Context) (T, error) {
		return value, nil
	}
}
