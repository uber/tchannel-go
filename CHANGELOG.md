Changelog
=========

# 1.0.0

* First stable release.
* Supports making calls with JSON, Thrift or raw payloads.
* Services use thrift-gen, and implement handlers wiht a `func(ctx, arg) (res,
  error)` signature.
* Supports retries.
* Peer selection (peer heap, prefer incoming strategy, for use with Hyperbahn).
* Graceful channel shutdown.
* TCollector trace reporter with sampling support.
* Metrics collection with StatsD.
* Thrift support, including includes.
