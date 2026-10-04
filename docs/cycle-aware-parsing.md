# Cycle-aware parsing

The C, Go, standalone Java and KickAssembler plugin Java encoders choose parses
using packed bytes first, then estimated extreme-decoder cycles. The stream
format, token ranges, assembly decoders, and SFX boot code are unchanged.

The primary weight is `1 << 32` per packed byte. This exceeds the total secondary
cost for a C64-sized input, so cycle preferences cannot trade away compression.
The secondary cost is twice the estimated token cycles, allowing half-cycle
per-byte terms without floating point:

| Token | Secondary cost, with output length `n` |
|---|---|
| Literal | `78 + 29*n + 5*(n & 1)` |
| RLE | `112 + 19*n - (n & 1)` |
| Special zeros | `112 + 19*n` |
| LZ2 | `148` |
| Short LZ | `156 + 31*n - 7*(n & 1)` |
| Long LZ | `196 + 31*n - 7*(n & 1)` |

These estimates include token dispatch, the copy loop, and pointer updates.
They account for length parity, but not page crossings. They guide selection;
the assembled decoder benchmark determines the actual speed change. A gain on
every benchmarked game is not a guarantee for every possible input or placement.

Java's on-demand literal edges call the same `literalCost()` helper as ordinary
literal tokens, avoiding a second, inconsistent cost formula. All implementations
retain their existing candidate discovery and path tie-breaking.

## Six-game result

Measured 2026-10-04 using the same games and source placement as KabutoCrunch.
Total output is 329,268 bytes. The decoder starts at `$0214`; the packed stream
ends at `$fff0`. Every output write and the final output are checked under py65,
and an independent format decoder checks each stream.

| Game | Packed before / after | Extreme cycles before | Extreme cycles after |
|---|---:|---:|---:|
| kabutocrunch | 12,500 / 12,500 | 753,769 | 747,596 |
| crackpots | 15,203 / 15,203 | 886,620 | 881,560 |
| abbey | 48,119 / 48,119 | 1,081,330 | 1,078,746 |
| fixit | 41,798 / 41,798 | 1,074,870 | 1,071,158 |
| pigquest | 15,599 / 15,599 | 675,887 | 675,015 |
| keystone | 17,669 / 17,669 | 1,078,408 | 1,069,881 |
| **Total** | **150,888 / 150,888** | **5,550,884** | **5,523,956** |

The extreme decoder improves by 0.485%. Regular cycles fall from 5,901,902 to
5,878,741; small cycles fall from 5,979,258 to 5,957,061. Each game improves with
each decoder. Decoder sizes remain 198 / 177 / 245 bytes.

`benchmarks/baseline.csv` records the previous parser, and
`benchmarks/cycle-aware.csv` records the updated parser.

## Validation

Build and compare all three standalone encoders, optionally including the
KickAssembler plugin API:

```powershell
python tools/verify_parsers.py --plugin-root ../TSCrunch-KickAssembler-plugin
python tools/benchmark_cycles.py
```

The parity check compares against revision `4511a75` by default; pass
`--baseline-ref` to select another historical encoder. It verifies byte-identical
C/Go/Java output across the six games and four synthetic boundary cases in raw,
inplace, SFX, stack-SFX, and screen-blanking SFX modes: 50 case/mode combinations.
The plugin's raw/inplace/SFX API is checked in 30 comparisons. Packed sizes do
not grow in these checks. Raw streams are independently decoded, and all six
games are verified through each assembled memory decoder by the benchmark.

The plugin's own `tools/verify_plugin.py` additionally checks the built JAR through
KickAssembler's actual modifier in raw, inplace and SFX modes. Build artifacts
are local validation outputs; version numbers and published release binaries
are unchanged.
