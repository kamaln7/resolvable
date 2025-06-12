# resolvable

A Go package for resolving values with retries, caching, and concurrency safety.

A "resolvable" takes the form of `func[T any](context.Context) (T, error)`. This package provides a few composable resolvables that allow you mix-and-match different behaviors as needed.

## Quick Start

Use `New(...)` to create a resolvable specifying the desired behaviors as options:

```go
op := func(ctx context.Context) ([]byte, error) {
    r, err := http.Get("api call")
    if err != nil {
        return nil, err
    }
    defer r.Body.Close()

    return ioutil.ReadAll(r.Body)
}

res := resolvable.New(op,
    resolvable.WithSafe(),                // add concurrency-safety
    resolvable.WithCacheTTL(time.Minute), // cache results for a minute
    resolvable.WithRetry(),               // do not cache errored results
    resolvable.WithGraceful(),            // return the last known good value on error
)
```

`New(...)` sets `WithSafe()` by default for concurrency safety. You may disable it by passing the `WithUnsafe()` option.

## Composables

Composables can also be used directly without `New()`.

### Once

Resolve a value once and cache the results forever.

```go
getRandomNumber := resolvable.Once(func(context.Context) (int, error) {
    return rand.IntN(100), nil
}).WithBackgroundContext()

// the first call will generate a random number
num1, err := getRandomNumber() // -> 42, nil

// subsequent calls will return the exact same result
num2, err := getRandomNumber() // -> 42, nil
num3, err := getRandomNumber() // -> 42, nil
```

### Retry

Resolve a value and cache it forever only if it was successful.

```go
bottleRubs := 0
rubGenieBottle := resolvable.Once(func(context.Context) (*Genie, error) {
    bottleRubs++
    if bottleRubs < 3 {
        return nil, errors.New("rub again")
    }

    return &Genie{}, nil
}).WithBackgroundContext()

genie, err := rubGenieBottle() // -> nil, error
genie, err := rubGenieBottle() // -> nil, error
genie, err := rubGenieBottle() // -> &Genie{}, nil

// subsequent calls will return the same &Genie{} instance.
```

### Cache

Resolve a value and cache for a specific period of time. Wrap it with [Safe](#safe) to add concurrency safety.

```go
getRandomNumber := resolvable.Cache(
    func(context.Context) (int, error) {
        return rand.IntN(100), nil
    },
    resolvable.CacheOpts{
        Expiry: time.Minute,
    },
).WithBackgroundContext()

// the first call will generate a random number and cache it for one minute
num1, err := getRandomNumber() // -> 42, nil
num2, err := getRandomNumber() // -> 42, nil

// one minute later...
num3, err := getRandomNumber() // -> 7, nil

// guard it with a mutex
concurrencySafe := resolvable.Safe(getRandomNumber)
```

### Graceful

Returns the last known good value on error.

```go
graceful := resolvable.Graceful(func(ctx context.Context) ([]byte, error) {
    // flakey network call
    r, err := http.Get("...")
    if err != nil {
        return nil, err
    }
    defer r.Body.Close()

    return io.ReadAll(r.Body)
})

res1 := graceful(ctx) // success    -> []byte{...}, nil
res2 := graceful(ctx) // http error -> []byte{cached res1 value}, error
res3 := graceful(ctx) // success    -> []byte{fresh value}, nil
```

### Safe

Guard a resovable with a mutex ensuring concurrency safety.

```go
safe := resolvable.Safe(func(context.Context) (any, error) {
    // code that interacts with a shared resource.
})

// safe() can be safely called concurrently.
```

### Static

A helper that returns a static value.

```go
a := resolvable.Static(123)

// syntactic sugar for:

a := func(context.Context) (int, error) {
    return 123, nil
}
```

## License

[MIT](/LICENSE)
