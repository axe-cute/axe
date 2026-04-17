# 🪓 axe Benchmark Results

> **Machine**: Apple M1 (8 cores) · macOS · Go 1.25 · `go test -bench -count=5`
>
> axe uses **Chi v5** as its HTTP router. All frameworks tested in-process via `httptest.NewRecorder()`,
> except Fiber which uses `app.Test()` (fasthttp internals).

---

## Summary

| Scenario | axe (Chi) | Gin | Echo | Fiber |
|---|---|---|---|---|
| **Static JSON** | **583 ns** · 12 allocs | 704 ns · 15 allocs | 792 ns · 15 allocs | 4,158 ns · 28 allocs |
| **URL Params** | **731 ns** · 14 allocs | 763 ns · 16 allocs | 762 ns · 15 allocs | 4,381 ns · 29 allocs |
| **Middleware** | **1,014 ns** · 19 allocs | 1,961 ns · 26 allocs | 1,980 ns · 24 allocs | 7,458 ns · 33 allocs |
| **JSON Parse** | 2,909 ns · 34 allocs | **2,914 ns** · 33 allocs | 2,883 ns · 33 allocs | 10,992 ns · 53 allocs |
| **Multi-Route** | 1,443 ns · 12 allocs | **747 ns** · 9 allocs | 626 ns · 10 allocs | 4,269 ns · 22 allocs |

> Values are **median** of 5 runs. Lower is better.

---

## Key Takeaways

### 🏆 axe (Chi) wins: Static JSON, URL Params, Middleware

Chi's radix-tree router + zero-allocation middleware design gives axe the fastest JSON response
and parameter extraction times. The middleware stack (Recoverer + RequestID + SetHeader) runs
at **~1 µs** — roughly **2× faster** than Gin and Echo.

### 🤝 Tie: JSON Body Parsing

All net/http frameworks parse JSON bodies at roughly the same speed (~2.9 µs).
This is dominated by `encoding/json`, not the router.

### ↕ Gin/Echo win: Multi-Route (50 routes)

Gin and Echo use faster radix-tree implementations for large route tables.
Chi's trie is slightly slower at **1.4 µs** vs **0.7 µs** (Gin) and **0.6 µs** (Echo).
In practice this ≤1 µs difference is negligible compared to DB/network latency.

### ⚠️ Fiber: fasthttp overhead in app.Test()

Fiber's numbers appear higher because `app.Test()` simulates a full HTTP connection
over a pipe (not just a function call). This is an apples-to-oranges comparison
for the micro-benchmark, though it's *fair* for "what your handler actually sees."

---

## Raw Data

```
goos: darwin
goarch: arm64
cpu: Apple M1

BenchmarkStaticJSON_Chi-8       2,046,088     582 ns/op     1,392 B/op   12 allocs/op
BenchmarkStaticJSON_Gin-8       1,667,936     704 ns/op     1,441 B/op   15 allocs/op
BenchmarkStaticJSON_Echo-8      1,392,001     792 ns/op     1,473 B/op   15 allocs/op
BenchmarkStaticJSON_Fiber-8       267,884   4,219 ns/op     6,042 B/op   28 allocs/op

BenchmarkURLParam_Chi-8         1,630,738     731 ns/op     1,728 B/op   14 allocs/op
BenchmarkURLParam_Gin-8         1,566,978     763 ns/op     1,457 B/op   16 allocs/op
BenchmarkURLParam_Echo-8        1,572,326     760 ns/op     1,473 B/op   15 allocs/op
BenchmarkURLParam_Fiber-8         276,348   4,234 ns/op     6,062 B/op   29 allocs/op

BenchmarkMiddleware_Chi-8       1,275,858     937 ns/op     1,881 B/op   19 allocs/op
BenchmarkMiddleware_Gin-8         659,338   1,819 ns/op     1,690 B/op   26 allocs/op
BenchmarkMiddleware_Echo-8        587,409   1,882 ns/op     1,887 B/op   24 allocs/op
BenchmarkMiddleware_Fiber-8       162,079   7,107 ns/op     6,140 B/op   33 allocs/op

BenchmarkJSONParse_Chi-8          417,339   2,844 ns/op     7,965 B/op   34 allocs/op
BenchmarkJSONParse_Gin-8          403,762   2,857 ns/op     7,667 B/op   33 allocs/op
BenchmarkJSONParse_Echo-8         427,630   2,829 ns/op     7,645 B/op   33 allocs/op
BenchmarkJSONParse_Fiber-8        119,805  10,545 ns/op    13,907 B/op   53 allocs/op

BenchmarkMultiRoute_Chi-8         836,367   1,386 ns/op     1,378 B/op   12 allocs/op
BenchmarkMultiRoute_Gin-8       1,740,336     667 ns/op     1,040 B/op    9 allocs/op
BenchmarkMultiRoute_Echo-8      1,969,442     606 ns/op     1,016 B/op   10 allocs/op
BenchmarkMultiRoute_Fiber-8       270,858   4,177 ns/op     5,846 B/op   22 allocs/op
```

## Reproduce

```bash
cd benchmarks
go test -bench=. -benchmem -count=5 -timeout=10m | tee results.txt
```
