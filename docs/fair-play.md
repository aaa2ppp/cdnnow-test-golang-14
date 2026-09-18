# Выравнивание условий для C и Rust кода

В исходном тестовом C и Rust были в неравных условиях: в C стоял `volatile`, который запрещал держать `x` в регистре
и заставлял компилятор выполнять цикл, но с реальными `load/store` на каждой итерации. В `Rust` `volatile` не было, 
и LLVM выбрасывал цикл как dead code вовсе. В результате Rust показывал 37 нс против 50 000 нс у C — разница в 1400 раз!

```
goos: linux
goarch: amd64
pkg: aaa2ppp/cdnnow-test-golang-14/internal/calc
cpu: Intel(R) Core(TM) i3-7100 CPU @ 3.90GHz
BenchmarkAdd-4             23497             50739 ns/op               0 B/op          0 allocs/op
BenchmarkSub-4          31563234                36.64 ns/op            0 B/op          0 allocs/op
```

Я привел оба примера к одинаковым условиям: убрал `volatile` из C, добавил `std::hint::black_box` в Rust.
После этого C и Rust показывают ~16 мкс, разница в четвертом знаке. Это подтверждает, что на одинаковом коде оба языка
дают одинаковую производительность.

```
goos: linux
goarch: amd64
pkg: aaa2ppp/cdnnow-test-golang-14/internal/calc
cpu: Intel(R) Core(TM) i3-7100 CPU @ 3.90GHz
BenchmarkAdd-4                     76219             15674 ns/op               0 B/op          0 allocs/op
BenchmarkSub-4                     75309             15660 ns/op               0 B/op          0 allocs/op
BenchmarkGoControlShot-4           75480             15500 ns/op               0 B/op          0 allocs/op
```

Обратите внимание: На свежих CPU разница может быть более заметна, но порядок цифр сохраняется.
```
goos: linux
goarch: amd64
pkg: aaa2ppp/cdnnow-test-golang-14/internal/calc
cpu: AMD Ryzen AI 7 350 w/ Radeon 860M              
BenchmarkAdd-16                    70101             15941 ns/op               0 B/op          0 allocs/op
BenchmarkSub-16                    99163             12002 ns/op               0 B/op          0 allocs/op
BenchmarkGoControlShot-16          94082             11947 ns/op               0 B/op          0 allocs/op
```

---

**PS**  
Для C `volatile` в последствии был восстановлен для "наглядности" тестов, `black_box` в Rust оставлен (оригинальный файл лежит рядом).

Но это не все! У этой истории было увлекательное [продолжение](./ryzen-volatile-mystery.md).
