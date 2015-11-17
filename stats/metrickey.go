// Copyright (c) 2015 Uber Technologies, Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package stats

import (
	"regexp"
	"strings"
)

// DefaultMetricPrefix is the default mapping for metrics to statsd keys.
func DefaultMetricPrefix(name string, tags map[string]string) string {
	parts := []string{"tchannel", name}

	var addKeys []string
	switch {
	case strings.HasPrefix(name, "outbound"):
		addKeys = append(addKeys, "service", "target-service", "target-endpoint")
		if strings.HasPrefix(name, "outbound.calls.retries") {
			addKeys = append(addKeys, "retry-count")
		}
	case strings.HasPrefix(name, "inbound"):
		addKeys = append(addKeys, "calling-service", "service", "endpoint")
	}

	for _, k := range addKeys {
		v, ok := tags[k]
		if !ok {
			v = "no-" + k
		}
		parts = append(parts, clean(v))
	}
	return strings.Join(parts, ".")
}

var specialChars = regexp.MustCompile(`[{}/\\:\s.]`)

// clean replaces special characters [{}/\\:\s.] with '-'
func clean(keyPart string) string {
	return specialChars.ReplaceAllString(keyPart, "-")
}
