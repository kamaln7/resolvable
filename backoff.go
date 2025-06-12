package resolvable

import "time"

// BackOff is a backoff policy for retrying an operation.
// see: https://github.com/cenkalti/backoff/tree/v5
type BackOff interface {
	// NextBackOff returns the duration to wait before retrying the operation,
	// backoff.Stop to indicate that no more retries should be made.
	//
	// Example usage:
	//
	//     duration := backoff.NextBackOff()
	//     if duration == backoff.Stop {
	//         // Do not retry operation.
	//     } else {
	//         // Sleep for duration and retry operation.
	//     }
	//
	NextBackOff() time.Duration

	// Reset to initial state.
	Reset()
}

// BackOffStop indicates that no more retries should be made for use in NextBackOff().
const BackOffStop time.Duration = -1

type zeroBackoff struct{}

func (b *zeroBackoff) Reset() {}

func (b *zeroBackoff) NextBackOff() time.Duration { return 0 }
