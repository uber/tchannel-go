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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/samuel/go-thrift/parser"
)

const tchannelThriftImport = "github.com/uber/tchannel-go/thrift"

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(in string) error {
	*s = append(*s, in)
	return nil
}

var (
	generateThrift     = flag.Bool("generateThrift", false, "Whether to generate all Thrift go code")
	apacheThriftImport = flag.String("thriftImport", "github.com/apache/thrift/lib/go/thrift", "Go package to use for the Thrift import")
	inputFile          = flag.String("inputFile", "", "The .thrift file to generate a client for")
	outputFile         = flag.String("outputFile", "", "The output file to generate go code to")
	nlSpaceNL          = regexp.MustCompile(`\n[ \t]+\n`)
	templates          stringSliceFlag
)

func init() {
	flag.Var(&templates, "template", "template file to compile code from")
}

type Template struct {
	template *template.Template
	name     string
}

// TemplateData is the data passed to the template that generates code.
type TemplateData struct {
	// input data information
	ThriftFilename string

	// golang package information
	Package        string
	ThriftImport   string
	TChannelImport string

	// datastructures defining the thrift service
	Services []*Service
	AST      *parser.Thrift
}

func main() {
	flag.Parse()

	if *inputFile == "" {
		log.Fatalf("Please specify an inputFile")
	}

	if err := processFile(*generateThrift, *inputFile, templates, *outputFile); err != nil {
		log.Fatal(err)
	}
}

func processFile(generateThrift bool, inputFile string, templateFiles []string, outputFile string) error {
	if generateThrift {
		if outFile, err := runThrift(inputFile, *apacheThriftImport); err != nil {
			return fmt.Errorf("Could not generate thrift output: %v", err)
		} else if outputFile == "" {
			outputFile = outFile
		}
	}

	parser := &parser.Parser{}
	parsed, _, err := parser.ParseFile(inputFile)
	if err != nil {
		return fmt.Errorf("Could not parse .thrift file: %v", err)
	}

	for thriftFilename, AST := range parsed {
		// TODO(prashant): Support multiple files / includes etc?
		// TODO migrate tchan template to template file in root of project

		wrappedServices, err := wrapServices(AST)
		if err != nil {
			log.Fatalf("Service parsing error: %v", err)
		}

		// The datastructure that is passed to the template
		templateData := TemplateData{
			ThriftFilename: thriftFilename,

			Package:        packageName(thriftFilename),
			ThriftImport:   *apacheThriftImport,
			TChannelImport: tchannelThriftImport,

			Services: wrappedServices,
			AST:      AST,
		}

		// load templates from template files
		parsedTemplates, err := getTemplatesFromFiles(templateFiles, AST)
		parsedTemplates = append(parsedTemplates, getTChanTemplate())

		if err != nil {
			return fmt.Errorf("could not parse template: %v", err)
		}

		for _, template := range parsedTemplates {
			if err := generateCode(outputFile, template, templateData); err != nil {
				return err
			}
		}
	}
	return nil
}

func getTChanTemplate() Template {
	return Template{
		template: parseTemplate(),
		name:     "tchan",
	}
}

func parseTemplate() *template.Template {
	funcs := map[string]interface{}{
		"contextType": contextType,
	}
	return template.Must(template.New("thrift-gen").Funcs(funcs).Parse(serviceTmpl))
}

func generateCode(outputFile string, t Template, td TemplateData) error {
	buf := &bytes.Buffer{}
	buf.WriteString("// @generated Code generated by thrift-gen. Do not modify.\n\n")

	if err := t.template.Execute(buf, td); err != nil {
		return fmt.Errorf("could not render template %s: %v", t.name, err)
	}

	// post process generated code
	generatedBytes := buf.Bytes()
	generatedBytes = cleanGeneratedCode(generatedBytes)

	// write file
	outputFilename := getOuputFilename(outputFile, td.ThriftFilename, t.name)
	if err := ioutil.WriteFile(outputFilename, generatedBytes, 0666); err != nil {
		return fmt.Errorf("cannot write output file %s: %v", outputFile, err)
	}

	// run gofmt over generated code to make it pretty
	exec.Command("gofmt", "-w", outputFilename).Run()
	return nil
}

func packageName(fullPath string) string {
	// TODO(prashant): Remove any characters that are not valid in Go package names.
	_, filename := filepath.Split(fullPath)
	file := strings.TrimSuffix(filename, filepath.Ext(filename))
	return strings.ToLower(file)
}

func cleanGeneratedCode(generated []byte) []byte {
	generated = nlSpaceNL.ReplaceAll(generated, []byte("\n"))
	return generated
}

func contextType() string {
	return "thrift.Context"
}

func getTemplateFromFile(filename string, AST *parser.Thrift) (*template.Template, error) {
	// export useful functions to the template renderer
	state := NewState(AST)
	funcs := map[string]interface{}{
		"contextType": contextType,
		"lowercase": func(in string) string {
			return strings.ToLower(in)
		},
		"goPublicName": goPublicName,
		"goType": func(t *parser.Type) string {
			return state.goType(t)
		},
	}

	// find which file to use for rendering
	filename, err := ResolveWithGopath(filename)
	if err != nil {
		return nil, err
	}

	// read template file
	templateBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	templateString := string(templateBytes)

	// parse template in usable form
	return template.Must(template.
		New("thrift-gen").
		Funcs(funcs).
		Parse(templateString)), nil
}

func getTemplatesFromFiles(files []string, AST *parser.Thrift) ([]Template, error) {
	var templates []Template

	for _, filename := range files {
		parsedTemplate, err := getTemplateFromFile(filename, AST)
		if err != nil {
			// PANIC!
			return nil, err
		} else {
			// add parsed template to templates
			templates = append(templates, Template{
				template: parsedTemplate,
				name:     filename,
			})
		}
	}
	return templates, nil
}

func outputDirectory(infile string) string {
	dir, _ := filepath.Split(infile)
	return dir
}

func getOuputFilename(outputFile string, thriftFile string, genFile string) string {
	dir := outputDirectory(outputFile)

	baseThriftFilename := getBasename(thriftFile)
	baseGenFilename := getBasename(genFile)

	return filepath.Join(dir, baseGenFilename+"-"+baseThriftFilename+".go")
}

func getBasename(path string) string {
	filename := filepath.Base(path)
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}
