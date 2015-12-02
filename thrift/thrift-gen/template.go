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

// thrift-gen generates code for Thrift services that can be used with the
// uber/tchannel/thrift package. thrift-gen generated code relies on the
// Apache Thrift generated code for serialization/deserialization, and should
// be a part of the generated code's package.
package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
	"text/template"
)

func parseTemplate() *template.Template {
	funcs := map[string]interface{}{
		"contextType": contextType,
	}
	return template.Must(template.New("thrift-gen").Funcs(funcs).Parse(tchannelTmpl))
}

func contextType() string {
	return "thrift.Context"
}

func cleanGeneratedCode(generated []byte) []byte {
	generated = nlSpaceNL.ReplaceAll(generated, []byte("\n"))
	return generated
}

func executeTemplate(outputFile string, tmpl *template.Template, td TemplateData) error {
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, td); err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}

	generated := cleanGeneratedCode(buf.Bytes())
	if err := ioutil.WriteFile(outputFile, generated, 0660); err != nil {
		return fmt.Errorf("cannot write output file %q: %v", outputFile, err)
	}

	// Run gofmt on the file (ignore any errors)
	exec.Command("gofmt", "-w", outputFile).Run()
	return nil
}
