package resolvable

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValue_Resolve(t *testing.T) {
	ctx := context.Background()
	t.Run("simple", func(t *testing.T) {
		v := New(func(ctx context.Context) (int, error) {
			return 1, nil
		})
		value, err := v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, value)
	})

	t.Run("retryable", func(t *testing.T) {
		var count int
		v := New(func(ctx context.Context) (int, error) {
			count++
			if count < 3 {
				return 0, errors.New("try again")
			}
			return count, nil
		}, WithRetry())

		value, err := v(ctx)
		require.EqualError(t, err, "try again")
		assert.Equal(t, 0, value)

		value, err = v(ctx)
		require.EqualError(t, err, "try again")
		assert.Equal(t, 0, value)

		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, value)

		// fn should not be called again
		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, value)
	})

	t.Run("expiry", func(t *testing.T) {
		now := time.Now()
		var (
			count      int
			resolveErr error
		)
		v := New(
			func(ctx context.Context) (int, error) {
				count++
				return count, resolveErr
			},
			WithCacheTTL(2*time.Second),
			WithNow(func() time.Time { return now }),
			WithRetry(),
		)

		value, err := v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, value)

		// still not expired
		now = now.Add(time.Second)
		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, value)

		// expired but resolves with an error
		now = now.Add(2 * time.Second)
		resolveErr = errors.New("resolve error")
		value, err = v(ctx)
		require.EqualError(t, err, "resolve error")
		assert.Equal(t, 2, value)

		// resolve again without error
		resolveErr = nil
		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, value) // the new value is returned
	})
}

func TestGraceful(t *testing.T) {
	ctx := context.Background()
	var (
		count      int
		resolveErr error
	)
	g := Graceful(Ctx[int](func(ctx context.Context) (int, error) {
		count++
		return count, resolveErr
	}))
	value, err := g(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, value)

	resolveErr = errors.New("resolve error")
	value, err = g(ctx)
	require.EqualError(t, err, "resolve error")
	assert.Equal(t, 1, value) // last known good value

	resolveErr = nil
	value, err = g(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, value) // new value
}

func TestOnce(t *testing.T) {
	ctx := context.Background()
	var count int
	o := New(
		func(ctx context.Context) (int, error) {
			count++
			return count, nil
		},
		WithOnce(),
	)
	value, err := o(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, value)

	value, err = o(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, value)
}

func TestTTL(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("cache errors", func(t *testing.T) {
		var (
			count      int
			resolveErr error
		)
		v := Cache(Ctx[int](func(ctx context.Context) (int, error) {
			count++
			return count, resolveErr
		}), CacheOpts{
			Expiry: 2 * time.Second,
			Now:    func() time.Time { return now },
		})

		value, err := v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, value)

		// still not expired
		now = now.Add(time.Second)
		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, value)

		// expired but resolves with an error
		now = now.Add(2 * time.Second)
		resolveErr = errors.New("resolve error")
		value, err = v(ctx)
		require.EqualError(t, err, "resolve error")
		assert.Equal(t, 2, value)

		// the error response is cached for the expiry duration
		resolveErr = nil
		value, err = v(ctx)
		require.EqualError(t, err, "resolve error")
		assert.Equal(t, 2, value) // the new value is returned

		// expired again but resolves without error
		now = now.Add(2 * time.Second)
		resolveErr = nil
		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, value)

		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, value)
	})

	t.Run("retry errors", func(t *testing.T) {
		var (
			count      int
			resolveErr error
		)
		v := Cache(Ctx[int](func(ctx context.Context) (int, error) {
			count++
			return count, resolveErr
		}), CacheOpts{
			Expiry: 2 * time.Second,
			Now:    func() time.Time { return now },
			Retry:  true,
		})

		// the clock never advances in this test
		resolveErr = errors.New("resolve error")
		value, err := v(ctx)
		require.EqualError(t, err, "resolve error")
		assert.Equal(t, 1, value)

		// we got an error before, so we need to resolve again
		value, err = v(ctx)
		require.EqualError(t, err, "resolve error")
		assert.Equal(t, 2, value)

		// we got an error before, so we need to resolve again
		resolveErr = nil
		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, value)

		// we did NOT get an error before, so we return the cached value
		value, err = v(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, value)
	})
}

func TestRetry(t *testing.T) {
	ctx := context.Background()
	var (
		count      int
		resolveErr error
	)
	var r Ctx[int]
	r = Retry(func(ctx context.Context) (int, error) {
		count++
		return count, resolveErr
	}, RetryOpts{})

	// resolve with error
	resolveErr = errors.New("try again")
	value, err := r(ctx)
	require.EqualError(t, err, "try again")
	assert.Equal(t, 1, value)

	value, err = r(ctx)
	require.EqualError(t, err, "try again")
	assert.Equal(t, 2, value)

	resolveErr = nil
	// resolve without error
	value, err = r(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, value)

	// the value is cached
	value, err = r(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, value)
}

func TestGracefulTTL(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	var (
		count      int
		resolveErr error
	)
	var g V[int]
	g = New(
		func(ctx context.Context) (int, error) {
			count++
			return count, resolveErr
		},
		WithCacheTTL(2*time.Second),
		WithNow(func() time.Time { return now }),
		WithGraceful(),
		WithUnsafe(),
		WithRetry(),
	).WithContext(ctx)

	// no error
	value, err := g()
	require.NoError(t, err)
	assert.Equal(t, 1, value)

	// cached value
	value, err = g()
	require.NoError(t, err)
	assert.Equal(t, 1, value)

	// expire & resolve with error
	now = now.Add(2 * time.Second)
	resolveErr = errors.New("resolve error")
	value, err = g()
	require.EqualError(t, err, "resolve error")
	assert.Equal(t, 1, value) // last known good value

	// resolve without error
	resolveErr = nil
	value, err = g()
	require.NoError(t, err)
	assert.Equal(t, 3, value)

	// expire & resolve without error
	now = now.Add(2 * time.Second)
	value, err = g()
	require.NoError(t, err)
	assert.Equal(t, 4, value)
}
