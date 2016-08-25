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

package mockhyperbahn

import (
	"testing"

	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
)

func TestAdvertisedTable(t *testing.T) {
	table := newAdvertisedTable()
	table.addPeer("a", "1")
	table.addPeer("a", "2")
	table.addPeer("a", "2")
	table.addPeer("a", "2")
	table.addPeer("c", "3")
	table.addPeer("d", "4")

	tests := []struct {
		svc  string
		want []string
	}{
		{
			svc:  "a",
			want: []string{"1", "2"},
		},
		{
			svc:  "c",
			want: []string{"3"},
		},
		// Service that would be between "a" and "c" in the sorted list.
		{
			svc: "b",
		},
		// Service that would be inserted at the end.
		{
			svc: "unknown",
		},
	}

	for _, tt := range tests {
		gotPeer, err := table.Get(testutils.FakeCallFrame{ServiceF: tt.svc}, nil)
		if len(tt.want) == 0 {
			assert.Error(t, err, "Expected error for %v", tt.svc)
			continue
		}

		assert.Len(t, table.serviceMapping[tt.svc], len(tt.want), "Unexpected number of peers in table for %v", tt.svc)
		assert.Contains(t, tt.want, gotPeer.HostPort, "Invalid host:port for %v", tt.svc)
	}
}
