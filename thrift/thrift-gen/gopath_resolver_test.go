package main

import (
	"os"
	"reflect"
	"testing"
)

var FileCandidatesWithGopathTests = []struct {
	GOPATH             string
	filename           string
	expectedCandidates []string
}{
	{
		GOPATH:   "onepath",
		filename: "github.com/uber/tchannel-go/tchan.thrift-gen",
		expectedCandidates: []string{
			"github.com/uber/tchannel-go/tchan.thrift-gen",
			"onepath/src/github.com/uber/tchannel-go/tchan.thrift-gen",
		},
	},
	{
		GOPATH:   "onepath:secondpath",
		filename: "github.com/uber/tchannel-go/tchan.thrift-gen",
		expectedCandidates: []string{
			"github.com/uber/tchannel-go/tchan.thrift-gen",
			"onepath/src/github.com/uber/tchannel-go/tchan.thrift-gen",
			"secondpath/src/github.com/uber/tchannel-go/tchan.thrift-gen",
		},
	},
}

func TestFileCandidatesWithGopath(t *testing.T) {
	for _, test := range FileCandidatesWithGopathTests {
		os.Setenv("GOPATH", test.GOPATH)
		candidates := FileCandidatesWithGopath(test.filename)
		if !reflect.DeepEqual(candidates, test.expectedCandidates) {
			t.Errorf("GOPATH=%s FileCandidatesWithGopath(%s) = %q, want %q", test.GOPATH, test.filename, candidates, test.expectedCandidates)
		}
	}
}
