<!-- SPDX-FileCopyrightText: 2026 gorfc community contributors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Benchmark Baselines

This file records SDK-free benchmark baselines used to compare later
performance work, especially the lazy/streaming table API.

## T4 baseline: public Call API with nwrfcmock

Command:

```bash
go test -tags nwrfc_nosdk -run '^$' -bench '^BenchmarkCall(SmallStruct|LargeTable_Materialize)$' -benchmem ./nwrfc
```

Environment:

| Field | Value |
|---|---|
| Date | 2026-05-14 |
| GOOS / GOARCH | linux / amd64 |
| CPU | 12th Gen Intel(R) Core(TM) i7-12650HX |
| Build tags | `nwrfc_nosdk` |
| SAP SDK | not used |
| SAP system | not used |

Results:

| Benchmark | Iterations | ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|
| `BenchmarkCallSmallStruct-20` | 1979863 | 678.3 | 416 | 3 |
| `BenchmarkCallLargeTable_Materialize-20` | 19659144 | 62.74 | 0 | 0 |

Interpretation:

- `BenchmarkCallSmallStruct` measures the public `nwrfc.Call` path for a
  small BAPI-like call with five scalar input parameters and five scalar
  outputs.
- `BenchmarkCallLargeTable_Materialize` measures the current public
  `nwrfc.CallMap` dispatch/materialized response contract with a prebuilt
  1000-row `[]map[string]any` fixture.
- Both benchmarks use `nwrfcmock`, so they measure Go-side marshaling,
  unmarshaling, locking, and mock dispatch overhead only. They do not prove
  SAP RFC runtime behavior and do not exercise the proprietary SDK.
- Mock response fixtures are built before `b.ResetTimer()` and reused by
  the timed handlers. These numbers intentionally exclude synthetic
  benchmark-payload construction cost.
