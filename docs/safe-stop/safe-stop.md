
```sh 
benchstat tmp/master-0.1.0-api.raw.txt tmp/safe-stop-api.raw.txt
```

```txt
goos: linux
goarch: amd64
pkg: aaa2ppp/cdnnow-test-golang-14/internal/api
cpu: Intel(R) Core(TM) i3-7100 CPU @ 3.90GHz
                                 │ tmp/master-0.1.0-api.raw.txt │     tmp/safe-stop-api.raw.txt      │
                                 │            sec/op            │   sec/op     vs base               │
API/std/stub_calc-4                                 49.91µ ± 0%   49.77µ ± 0%  -0.27% (p=0.001 n=30)
API/std/sync_calc-4                                 121.8µ ± 0%   121.6µ ± 0%  -0.21% (p=0.004 n=30)
API/std/async_calc-4                                72.31µ ± 0%   72.35µ ± 0%  +0.05% (p=0.004 n=30)
API/std/parallel_calc-4                             53.54µ ± 0%   53.55µ ± 0%       ~ (p=0.582 n=30)
API/fast/stub_calc-4                                23.27µ ± 0%   23.39µ ± 0%  +0.52% (p=0.002 n=30)
API/fast/sync_calc-4                                90.38µ ± 0%   90.68µ ± 0%  +0.33% (p=0.000 n=30)
API/fast/async_calc-4                               71.04µ ± 0%   70.90µ ± 0%       ~ (p=0.264 n=30)
API/fast/parallel_calc-4                            56.30µ ± 0%   56.19µ ± 0%  -0.20% (p=0.000 n=30)
APIParallel/std/stub_calc-4                         16.93µ ± 1%   16.89µ ± 0%       ~ (p=0.626 n=30)
APIParallel/std/sync_calc-4                         90.37µ ± 0%   89.32µ ± 0%  -1.16% (p=0.000 n=30)
APIParallel/std/async_calc-4                        72.39µ ± 0%   72.50µ ± 0%  +0.16% (p=0.000 n=30)
APIParallel/std/parallel_calc-4                     54.69µ ± 0%   54.76µ ± 0%       ~ (p=0.151 n=30)
APIParallel/fast/stub_calc-4                        13.80µ ± 0%   13.81µ ± 0%       ~ (p=0.359 n=30)
APIParallel/fast/sync_calc-4                        76.03µ ± 0%   75.82µ ± 0%  -0.28% (p=0.000 n=30)
APIParallel/fast/async_calc-4                       70.99µ ± 0%   71.03µ ± 0%       ~ (p=0.641 n=30)
APIParallel/fast/parallel_calc-4                    56.40µ ± 0%   56.36µ ± 0%       ~ (p=0.099 n=30)
geomean                                             53.88µ        53.83µ       -0.08%

                                 │ tmp/master-0.1.0-api.raw.txt │       tmp/safe-stop-api.raw.txt        │
                                 │             B/op             │     B/op       vs base                 │
API/std/stub_calc-4                              1.822Ki ± 0%     1.823Ki ±  0%  +0.05% (p=0.002 n=30)
API/std/sync_calc-4                              1.824Ki ± 0%     1.825Ki ±  0%       ~ (p=0.215 n=30)
API/std/async_calc-4                             1.824Ki ± 0%     1.824Ki ±  0%       ~ (p=0.129 n=30)
API/std/parallel_calc-4                          1.812Ki ± 0%     1.811Ki ±  0%  -0.05% (p=0.000 n=30)
API/fast/stub_calc-4                               0.000 ± 0%       0.000 ±  0%       ~ (p=1.000 n=30) ¹
API/fast/sync_calc-4                               0.000 ±  ?       0.000 ±   ?       ~ (p=0.802 n=30)
API/fast/async_calc-4                              0.000 ± 0%       0.000 ±  0%       ~ (p=1.000 n=30) ¹
API/fast/parallel_calc-4                           0.000 ± 0%       0.000 ±  0%       ~ (p=1.000 n=30) ¹
APIParallel/std/stub_calc-4                      1.835Ki ± 0%     1.837Ki ±  0%  +0.11% (p=0.000 n=30)
APIParallel/std/sync_calc-4                      1.845Ki ± 0%     1.843Ki ±  0%  -0.11% (p=0.011 n=30)
APIParallel/std/async_calc-4                     1.839Ki ± 0%     1.838Ki ±  0%       ~ (p=0.147 n=30)
APIParallel/std/parallel_calc-4                  1.838Ki ± 0%     1.837Ki ±  0%       ~ (p=0.086 n=30)
APIParallel/fast/stub_calc-4                       2.000 ± 0%       2.000 ± 50%       ~ (p=0.790 n=30)
APIParallel/fast/sync_calc-4                       6.000 ± 0%       6.000 ± 17%       ~ (p=0.697 n=30)
APIParallel/fast/async_calc-4                      6.000 ± 0%       6.000 ±  0%       ~ (p=0.792 n=30)
APIParallel/fast/parallel_calc-4                   7.000 ± 0%       7.000 ±  0%       ~ (p=0.584 n=30)
geomean                                                       ²                  -0.00%                ²
¹ all samples are equal
² summaries must be >0 to compute geomean

                                 │ tmp/master-0.1.0-api.raw.txt │      tmp/safe-stop-api.raw.txt      │
                                 │          allocs/op           │ allocs/op   vs base                 │
API/std/stub_calc-4                                17.00 ± 6%     17.00 ± 6%       ~ (p=0.799 n=30)
API/std/sync_calc-4                                17.00 ± 0%     17.00 ± 0%       ~ (p=0.552 n=30)
API/std/async_calc-4                               17.00 ± 0%     17.00 ± 0%       ~ (p=0.237 n=30)
API/std/parallel_calc-4                            16.00 ± 0%     16.00 ± 0%       ~ (p=1.000 n=30) ¹
API/fast/stub_calc-4                               0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
API/fast/sync_calc-4                               0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
API/fast/async_calc-4                              0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
API/fast/parallel_calc-4                           0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/std/stub_calc-4                        17.00 ± 0%     17.00 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/std/sync_calc-4                        17.00 ± 0%     17.00 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/std/async_calc-4                       17.00 ± 0%     17.00 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/std/parallel_calc-4                    17.00 ± 0%     17.00 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/fast/stub_calc-4                       0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/fast/sync_calc-4                       0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/fast/async_calc-4                      0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
APIParallel/fast/parallel_calc-4                   0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=30) ¹
geomean                                                       ²               +0.00%                ²
¹ all samples are equal
² summaries must be >0 to compute geomean
```

```sh
benchstat master-0.1.0-calc.raw.txt safe-stop-calc.raw.txt
```

```txt
goos: linux
goarch: amd64
pkg: aaa2ppp/cdnnow-test-golang-14/internal/calculators
cpu: Intel(R) Core(TM) i3-7100 CPU @ 3.90GHz
                           │ tmp/master-0.1.0-calc.raw.txt │     tmp/safe-stop-calc.raw.txt     │
                           │            sec/op             │   sec/op     vs base               │
Sync                                           66.26µ ± 0%   66.23µ ± 0%       ~ (p=0.372 n=20)
Sync-2                                         66.33µ ± 0%   66.33µ ± 0%       ~ (p=0.616 n=20)
Async                                          66.36µ ± 0%   66.35µ ± 0%       ~ (p=0.713 n=20)
Async-2                                        67.87µ ± 0%   67.87µ ± 0%       ~ (p=0.723 n=20)
AsyncSingle/Add_C_lib                          50.71µ ± 0%   50.67µ ± 0%       ~ (p=0.071 n=20)
AsyncSingle/Add_C_lib-2                        52.20µ ± 0%   52.21µ ± 0%       ~ (p=0.224 n=20)
AsyncSingle/Sub_Rust_lib                       15.86µ ± 0%   15.85µ ± 0%       ~ (p=0.673 n=20)
AsyncSingle/Sub_Rust_lib-2                     17.37µ ± 0%   17.40µ ± 0%  +0.18% (p=0.004 n=20)
Parallel                                       65.14µ ± 1%   65.26µ ± 0%       ~ (p=0.344 n=20)
Parallel-2                                     52.43µ ± 0%   52.36µ ± 0%       ~ (p=0.449 n=20)
geomean                                        46.70µ        46.70µ       +0.01%

                           │ tmp/master-0.1.0-calc.raw.txt │      tmp/safe-stop-calc.raw.txt       │
                           │             B/op              │    B/op     vs base                   │
Sync                                          0.000 ± 0%     0.000 ± 0%         ~ (p=1.000 n=20) ¹
Sync-2                                        0.000 ± 0%     0.000 ± 0%         ~ (p=1.000 n=20) ¹
Async                                         0.000 ± 0%     1.000 ± 0%         ? (p=0.000 n=20)
Async-2                                       0.000 ± 0%     1.000 ± 0%         ? (p=0.000 n=20)
AsyncSingle/Add_C_lib                         0.000 ± 0%     0.000 ± 0%         ~ (p=1.000 n=20) ¹
AsyncSingle/Add_C_lib-2                       0.000 ± 0%     0.000 ± 0%         ~ (p=1.000 n=20) ¹
AsyncSingle/Sub_Rust_lib                      0.000 ± 0%     0.000 ± 0%         ~ (p=1.000 n=20) ¹
AsyncSingle/Sub_Rust_lib-2                    0.000 ± 0%     0.000 ± 0%         ~ (p=1.000 n=20) ¹
Parallel                                      1.000 ± 0%     2.000 ± 0%  +100.00% (p=0.000 n=20)
Parallel-2                                    0.000 ± 0%     1.000 ± 0%         ? (p=0.000 n=20)
geomean                                                  ²               ?                       ²
¹ all samples are equal
² summaries must be >0 to compute geomean

                           │ tmp/master-0.1.0-calc.raw.txt │     tmp/safe-stop-calc.raw.txt      │
                           │           allocs/op           │ allocs/op   vs base                 │
Sync                                          0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
Sync-2                                        0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
Async                                         0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
Async-2                                       0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
AsyncSingle/Add_C_lib                         0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
AsyncSingle/Add_C_lib-2                       0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
AsyncSingle/Sub_Rust_lib                      0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
AsyncSingle/Sub_Rust_lib-2                    0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
Parallel                                      0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
Parallel-2                                    0.000 ± 0%     0.000 ± 0%       ~ (p=1.000 n=20) ¹
geomean                                                  ²               +0.00%                ²
¹ all samples are equal
² summaries must be >0 to compute geomean
```