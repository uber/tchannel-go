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

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This test ensures that the code generator generates valid code that can be built
// in combination with Thrift's autogenerated code.

func TestAllThrift(t *testing.T) {
	files, err := ioutil.ReadDir("test_files")
	if err != nil {
		t.Fatalf("Cannot read test_files directory: %v", err)
	}

	for _, f := range files {
		fname := f.Name()
		if filepath.Ext(fname) != ".thrift" {
			continue
		}

		if err := runTest(t, filepath.Join("test_files", f.Name())); err != nil {
			t.Errorf("Thrift file %v failed: %v", f.Name(), err)
		}
	}
}

func copyFile(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	writeF, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer writeF.Close()

	_, err = io.Copy(writeF, f)
	return err
}

// setupDirectory creates a temporary directory and copies the Thrift file into that directory.
func setupDirectory(thriftFile string) (string, string, error) {
	tempDir, err := ioutil.TempDir("", "thrift-gen")
	if err != nil {
		return "", "", err
	}

	outFile := filepath.Join(tempDir, "test.thrift")
	return tempDir, outFile, copyFile(thriftFile, outFile)
}

func runTest(t *testing.T, thriftFile string) error {
	tempDir, thriftFile, err := setupDirectory(thriftFile)
	if err != nil {
		return err
	}

	// Generate code from the Thrift file.
	t.Logf("runTest in %v", tempDir)
	if err := processFile(true /* generateThrift */, thriftFile, nil, ""); err != nil {
		return fmt.Errorf("processFile(%s) failed: %v", thriftFile, err)
	}

	// Run go build to ensure that the generated code builds.
	cmd := exec.Command("go", "build", ".")
	cmd.Dir = filepath.Join(tempDir, "gen-go", "test")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Build failed. Output = \n%v\n", string(output))
	}

	// Only delete the temp directory on success.
	os.RemoveAll(tempDir)
	return nil
}
