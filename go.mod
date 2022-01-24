module github.com/uber/tchannel-go

go 1.16

require (
	github.com/HdrHistogram/hdrhistogram-go v1.1.2 // indirect
	// github.com/apache/thrift should be >=0.9.3, <0.11.0 due to
	// https://issues.apache.org/jira/browse/THRIFT-5205
	// We are currently pinned to b2a4d4ae21c789b689dd162deb819665567f481c
	github.com/apache/thrift v0.0.0-20161221203622-b2a4d4ae21c7
	github.com/bmizerany/perks v0.0.0-20141205001514-d9a9656a3a4b
	github.com/cactus/go-statsd-client/v5 v5.0.0
	github.com/crossdock/crossdock-go v0.0.0-20160816171116-049aabb0122b
	github.com/jessevdk/go-flags v1.5.0
	github.com/opentracing/opentracing-go v1.1.0
	github.com/prashantv/protectmem v0.0.0-20171002184600-e20412882b3a
	github.com/samuel/go-thrift v0.0.0-20190219015601-e8b6b52668fe
	github.com/streadway/quantile v0.0.0-20150917103942-b0c588724d25
	github.com/stretchr/testify v1.7.0
	github.com/uber-go/tally v3.3.15+incompatible
	github.com/uber/jaeger-client-go v2.22.1+incompatible
	github.com/uber/jaeger-lib v2.4.1+incompatible // indirect
	go.uber.org/atomic v1.7.0
	go.uber.org/multierr v1.7.0
	golang.org/x/net v0.0.0-20190926025831-c00fd9afed17
	golang.org/x/sys v0.0.0-20210320140829-1e4c9ba3b0c4
	gopkg.in/yaml.v2 v2.4.0
)
