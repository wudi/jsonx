# jsonx

A high-performance JSON library for Go with a drop-in `encoding/json` API.

> Powered by AI + [autoresearch](https://github.com/karpathy/autoresearch) automation. I don't know how the fuck it works, but it's fast as hell.

---

## Benchmarks

Decode beats `bytedance/sonic` by ≥ 10 % on all 11 gates (amd64, AVX-512BW):
struct workloads cluster at +50–88 %, `interface{}` at +10–63 %, at a
fraction of sonic's memory footprint. ARM64 (Neoverse-N1) struct decode
runs +11–14 % over sonic at ~11 % of its resident memory.

Full scorecard — every corpus, encode figures, stdlib and `goccy/go-json`
comparisons — lives in [RESULTS.md](RESULTS.md).

## Install

```sh
go get github.com/wudi/jsonx
```

Requires Go 1.22+.

## Usage

The package surface mirrors `encoding/json`:

```go
import "github.com/wudi/jsonx"

type User struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

var u User
err := jsonx.Unmarshal(data, &u)
out, err := jsonx.Marshal(u)

if !jsonx.Valid(data) { ... }

dec := jsonx.NewDecoder(r)
enc := jsonx.NewEncoder(w)
```

Same struct tags, same interface target types, same byte output up to
shortest-representation equivalence.

## Compatibility

- **linux/amd64, darwin/amd64** — AVX-512BW string scan, whitespace skipper,
  and digit-emission kernels. Runtime-gated on `cpuid`.
- **linux/arm64, darwin/arm64** — NEON + scalar arm64 kernels.
- **Other `GOOS`/`GOARCH`** — pure-Go SWAR fallback.

Assembly kernels are gated on a runtime CPU-feature check; absent the
feature, the library falls through to pure Go without a rebuild.

## How it works

Highlights (see [program.md](program.md) for the full experiment log):

- Type-specialized decoder plan cache — one compiled function per
  `reflect.Type`, struct fields written via `unsafe.Add(ptr, offset)`.
- 8-byte prefix field dispatch replaces FNV-1a on the struct hot path.
- Slab-allocated `interface{}` boxes collapse hundreds of tiny `mallocgc`
  calls into one geometric-grown allocation.
- AVX-512BW string scan (23.8 GB/s on EPYC-Genoa) and whitespace skipper
  (single `VPCMPUB $6` per 64-byte chunk); NEON equivalents via `CMHI`.
- Clinger + Eisel-Lemire float parsing; Schubfach formatting (BSL-1.0,
  ~41 % faster than `strconv.AppendFloat`).
- Encoder with one allocation per call: pooled `[]byte`, pre-quoted
  struct keys, inlined `encodeAny` type switch.

## Tests & benchmarks

```sh
go test ./...
cd bench && go test -bench=. -benchtime=3s -count=5 -benchmem
```

Coverage includes full-corpus round-trip vs `encoding/json`, dedicated
correctness tests for each SIMD kernel, and an unconditional pure-Go
`fallback_test.go`.

## When to reach for something else

`jsonx` targets server workloads where payloads are large enough (> ~64
bytes) to amortize SIMD costs and structs are reused often enough to
amortize plan-cache warmup. For short-lived processes or tiny payloads,
`encoding/json` is often already fast enough. `bytedance/sonic` remains
excellent when its JIT codegen and native structural indexer apply —
notably on very large pretty-printed decode.

## Credits

- **Schubfach** — Alexander Bolz, *Drachennest* (BSL-1.0). Go port derived
  from ByteDance sonic's `native/f64toa.c`.
- **Eisel-Lemire `detailedPowersOfTen`** — ported from Go's `strconv`.
- **Avo** — amd64 kernels generated with
  [`mmcloughlin/avo`](https://github.com/mmcloughlin/avo).
- **Corpora** — `twitter.json`, `citm_catalog.json`, `canada.json` from
  nativejson-benchmark; `small.json` and formatted synthetics are local.

Prior art: [`bytedance/sonic`](https://github.com/bytedance/sonic),
[`goccy/go-json`](https://github.com/goccy/go-json),
[`encoding/json`](https://pkg.go.dev/encoding/json).

## License
MIT
