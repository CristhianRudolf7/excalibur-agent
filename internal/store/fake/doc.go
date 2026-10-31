// Package fake provides in-memory implementations of the store.Store
// contract for unit tests of other packages. It records every write
// command and read query, serves scripted rows and keeps blobs in
// memory. It is not durable and not concurrent-safe beyond a mutex.
package fake
