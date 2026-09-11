#include "calculator.h"

uint64_t X;

int64_t add(int64_t a, int64_t b) {
    uint64_t x = (uint64_t)(a + b);

    for (int i = 0; i < 10000; i++) {
        x ^= x << 13;
        x ^= x >> 17;
        x ^= x << 5;
    }

    X = x;

    return a + b;
}
