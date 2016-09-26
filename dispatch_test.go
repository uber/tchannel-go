package tchannel

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGoroutineDispatcherHandlesPanic tests that the Dispatch method of the goroutineDispatcher
// handles panics within the provided f.
func TestGoroutineDispatcherHandlesPanic(t *testing.T) {
	var buf bytes.Buffer
	d := NewGoroutineDispatcher(NewLogger(&buf))
	call := InboundCall{
		serviceName:  "sampleService",
		methodString: "sampleMethod",
		headers: transportHeaders{
			CallerName: "sampleCaller",
		},
	}
	d.Dispatch(&call, func() {
		panic("panicString")
	})
	bufStr := buf.String()
	assert.Contains(t, bufStr, "panicString")
	assert.Contains(t, bufStr, "sampleService")
	assert.Contains(t, bufStr, "sampleMethod")
	assert.Contains(t, bufStr, "sampleCaller")
}
