package main

import (
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileCandidatesWithGopathSingleGopath(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Error(err)
	}
	log.Printf("Error: %v", err)

	os.Setenv("GOPATH", wd)

	files := FileCandidatesWithGopath("github.com/uber/tchannel-go/tchan.thrift-gen")

	if !reflect.DeepEqual(files, []string{
		"github.com/uber/tchannel-go/tchan.thrift-gen",
		filepath.Join(wd, "src", "github.com/uber/tchannel-go/tchan.thrift-gen"),
	}) {
		t.Error("Expected different candidates")
	}
}

func TestFileCandidatesWithGopathMultipleGopath(t *testing.T) {
	wd, err := os.Getwd()
	godeps := filepath.Join(wd, "Godeps")

	if err != nil {
		t.Error(err)
	}
	log.Printf("Error: %v", err)

	os.Setenv("GOPATH", wd+":"+godeps)

	files := FileCandidatesWithGopath("github.com/uber/tchannel-go/tchan.thrift-gen")

	if !reflect.DeepEqual(files, []string{
		"github.com/uber/tchannel-go/tchan.thrift-gen",
		filepath.Join(wd, "src", "github.com/uber/tchannel-go/tchan.thrift-gen"),
		filepath.Join(wd, "Godeps", "src", "github.com/uber/tchannel-go/tchan.thrift-gen"),
	}) {
		t.Error("Expected different candidates")
	}
}
