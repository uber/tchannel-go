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
	"os"
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
	skipTChannel       = flag.Bool("skipTChannel", false, "Do not compile tchannel. If you still want tchannel code to be generated pass in the tchannel template")
	apacheThriftImport = flag.String("thriftImport", "github.com/apache/thrift/lib/go/thrift", "Go package to use for the Thrift import")
	inputFile          = flag.String("inputFile", "", "The .thrift file to generate a client for")
	outputFile         = flag.String("outputFile", "", "The output file to generate go code to")
	ouptutDirectory    = flag.String("outputDir", "", "The output directory to generate go code to")
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

	// if output directory is not set use the working directory
	if *ouptutDirectory == "" {
		*ouptutDirectory, _ = os.Getwd()

		// for backwards compatibility take the directory of the outputFile
		if *outputFile != "" {
			log.Printf("--outputFile is deprecated, use --outputFile instead. Using the directory of --outputFile as --ouptutDirectory")
			*ouptutDirectory, _ = filepath.Split(*outputFile)
		}
	}

	// find the name of the thrift file for output

	if _, err := processFile(*generateThrift, *inputFile, *skipTChannel, templates, *ouptutDirectory); err != nil {
		log.Fatal(err)
	}
}

func processFile(generateThrift bool, inputFile string, skipTChannel bool, templateFiles []string, outputDirectory string) ([]string, error) {
	if generateThrift {
		// TODO make runThrift output to output directory
		if err := runThrift(inputFile, *apacheThriftImport, outputDirectory); err != nil {
			return nil, fmt.Errorf("Could not generate thrift output: %v", err)
		}
	}

	parser := &parser.Parser{}
	parsed, _, err := parser.ParseFile(inputFile)
	if err != nil {
		return nil, fmt.Errorf("Could not parse .thrift file: %v", err)
	}

	var generatedFiles []string

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
		if !skipTChannel {
			// append the tchannel template to the template if it should not be skipped
			parsedTemplates = append(parsedTemplates, getTChanTemplate())
		}

		if err != nil {
			return nil, fmt.Errorf("could not parse template: %v", err)
		}

		for _, template := range parsedTemplates {
			if generatedFile, err := generateCode(outputDirectory, template, templateData); err != nil {
				return nil, err
			} else {
				generatedFiles = append(generatedFiles, generatedFile)
			}
		}
	}
	return generatedFiles, nil
}

func generateCode(outputDirectory string, t Template, td TemplateData) (string, error) {
	buf := &bytes.Buffer{}
	buf.WriteString("// @generated Code generated by thrift-gen. Do not modify.\n\n")

	if err := t.template.Execute(buf, td); err != nil {
		return "", fmt.Errorf("could not render template %s: %v", t.name, err)
	}

	// post process generated code
	generatedBytes := buf.Bytes()
	generatedBytes = cleanGeneratedCode(generatedBytes)

	// write file
	outputFilename := getOuputFilename(outputDirectory, td.ThriftFilename, t.name)
	if err := ioutil.WriteFile(outputFilename, generatedBytes, 0666); err != nil {
		return "", fmt.Errorf("cannot write output file %s: %v", outputFile, err)
	}

	// run gofmt over generated code to make it pretty
	exec.Command("gofmt", "-w", outputFilename).Run()
	return outputFilename, nil
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

func getTChanTemplate() Template {
	funcs := map[string]interface{}{
		"contextType": contextType,
	}

	return Template{
		template: template.Must(template.New("thrift-gen").Funcs(funcs).Parse(serviceTmpl)),
		name:     "tchan",
	}
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

func getOuputFilename(outputDirectory string, thriftFile string, templateFile string) string {
	baseThriftFile := getBasename(thriftFile)
	baseTemplateFile := getBasename(templateFile)

	return filepath.Join(outputDirectory, baseThriftFile, baseTemplateFile+"-"+baseThriftFile+".go")
}

func getBasename(path string) string {
	filename := filepath.Base(path)
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}
