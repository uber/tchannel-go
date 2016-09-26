package tchannel

import (
	"fmt"
	"runtime"
)

// Dispatcher is an interface that dispatches a request to a handler
type Dispatcher interface {
	Dispatch(call *InboundCall, f func())
}

// recoverFn handles the dispatched call if it panics
func recoverFn(logger Logger, call *InboundCall) {
	if r := recover(); r != nil {
		// log panic
		const size = 64 << 10
		buf := make([]byte, size)
		buf = buf[:runtime.Stack(buf, false)]
		panicStr := fmt.Sprintf("%v", r)
		log := logger.WithFields(
			LogField{"panic", panicStr},
			LogField{"serviceName", call.ServiceName()},
			LogField{"method", call.MethodString()},
			LogField{"callerName", call.CallerName()},
			LogField{"stack", buf},
		)
		log.Error("request handler panicked")

		sendError(call)
	}
}

// sendError returns a system error to the client with some info about the panic
func sendError(call *InboundCall) {
	response := call.Response()
	err := NewSystemError(
		ErrCodeUnexpected,
		"request handler %q:%q panicked while processing request",
		call.ServiceName(),
		call.Method(),
	)
	response.SendSystemError(err)
}

// goroutineDispatcher is a dispatcher that uses a separate goroutine for each request with no limits
type goroutineDispatcher struct {
	logger Logger
}

// NewGoroutineDispatcher creates a new dispatcher that uses a new goroutine for each request
func NewGoroutineDispatcher(logger Logger) Dispatcher {
	return &goroutineDispatcher{
		logger: logger,
	}
}

// Dispatch calls the provided function in a separate goroutine for each call
func (d *goroutineDispatcher) Dispatch(call *InboundCall, f func()) {
	go func() {
		defer recoverFn(d.logger, call)
		f()
	}()
}
