---
name: redis-operations
description: "Use Redis as cache, queue and key-value store from redis-cli with safe inspection habits."
metadata:
  excalibur:
    group: databases
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Redis operations
      name_es: Operación de Redis
      desc_en: "Use Redis as cache, queue and key-value store from redis-cli with safe inspection habits."
      desc_es: "Redis como caché, cola y KV desde redis-cli con inspección segura."
---
# Redis operations

Use this skill for caching, queues, counters and ephemeral state in Redis. Inspect with `SCAN`, never `KEYS`, on any dataset that matters.

## When to use

- Running a local Redis for development or tests.
- Debugging cache misses, stale values or queue backlogs.
- Choosing data structures (strings, hashes, lists, sets, sorted sets).

## Procedure

1. Start: distro package, `docker run -p 6379:6379 redis:7`, or the test harness's instance; check with `redis-cli ping` (expect `PONG`).
2. Inspect safely: `SCAN 0 TYPE string` (paginated) instead of `KEYS *`; `TTL <key>` for expiry, `MEMORY USAGE <key>` for size suspects.
3. Operate: `SET k v EX 3600` (always set expiry for caches), `HSET/HGETALL` for objects, `LPUSH/BRPOP` for queues, `INCR` for counters, `ZADD/ZRANGE` for leaderboards.
4. Persist deliberately: RDB snapshots vs AOF logs trade durability for speed; for throwaway dev data, persistence off is fine and fast.
5. Require auth in shared environments (`requirepass` + `AUTH`); never expose an unauthenticated Redis to a network.

## Pitfalls

- `KEYS *` blocks the server on large datasets; `SCAN` iterates without blocking.
- No expiry means unbounded growth; every cache write gets a TTL.
- `FLUSHALL` deletes every database on the instance; scope deletes with patterns plus `UNLINK` instead.

## Verification

- `redis-cli ping` returns `PONG`, the app's cache hit path works (`MONITOR` shows expected commands during a test), and memory stays bounded.
