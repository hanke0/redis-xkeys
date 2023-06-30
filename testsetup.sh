#!/usr/bin/env bash

set -e

for ((i = 0; i < 100000; i++)); do
    redis-cli set "s-$i" "s$i" >/dev/null
    redis-cli set "i-$i" "$i" >/dev/null
    redis-cli zadd "z-$i" "$i" "$i" >/dev/null
    redis-cli hset "h-$i" "$i" "$i" >/dev/null
done
