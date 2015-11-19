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

var (
	generateThrift     = flag.Bool("generateThrift", false, "Whether to generate all Thrift go code")
	apacheThriftImport = flag.String("thriftImport", "github.com/apache/thrift/lib/go/thrift", "Go package to use for the Thrift import")
	inputFile          = flag.String("inputFile", "", "The .thrift file to generate a client for")
	outputFile         = flag.String("outputFile", "", "The output file to generate go code to")
	dynamic            = flag.Bool("enableDynamic", false, "Generate go code for all *.thrift-gen files in the $GOPATH")
	nlSpaceNL          = regexp.MustCompile(`\n[ \t]+\n`)
)

// TemplateData is the data passed to the template that generates code.
type TemplateData struct {
	Package        string
	Services       []*Service
	ThriftImport   string
	TChannelImport string
}

type GenericTemplateDate struct {
	Package string
	AST     *parser.Thrift
}

func main() {
	flag.Parse()
	if *inputFile == "" {
		log.Fatalf("Please specify an inputFile")
	}

	if err := processFile(*generateThrift, *inputFile, *outputFile); err != nil {
		log.Fatal(err)
	}
}

func processFile(generateThrift bool, inputFile string, outputFile string) error {
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

	goTmpl := parseTemplate() // tchan-template

	templateFiles, err := findThriftGenFiles("") // get files in $GOPATH
	if err != nil {
		return fmt.Errorf("Could not find thrift-gen files: %v", err)
	}

	templates, err := getTemplatesFromFiles(templateFiles)
	if err != nil {
		return fmt.Errorf("Could not parse thrift-gen files: %v", err)
	}

	for thriftFilename, AST := range parsed {
		if err := generateCode(outputFile, goTmpl, packageName(thriftFilename), AST); err != nil {
			return err
		}

		if *dynamic {
			for templateFilename, template := range templates {
				buf := &bytes.Buffer{}

				if err := template.Execute(buf, &GenericTemplateDate{
					Package: packageName(thriftFilename),
					AST:     AST,
				}); err != nil {
					return fmt.Errorf("Could not parse thrift-gen template: %v", err)
				}

				fmt.Println("Have some code!")

				// post process generated code
				generatedBytes := buf.Bytes()
				generatedBytes = cleanGeneratedCode(generatedBytes)

				// do useful stuff with generated code
				outputFilename := getOuputFilename(thriftFilename, AST, templateFilename)
				fmt.Println(string(generatedBytes))

				// write file
				if err := ioutil.WriteFile(outputFilename, generatedBytes, 0666); err != nil {
					return fmt.Errorf("cannot write output file %s: %v", outputFile, err)
				} else {
					fmt.Printf("Saved file: %s\n", outputFilename)
				}

				exec.Command("gofmt", "-w", outputFilename).Run()
			}
		}

	}

	return nil
}

func parseTemplate() *template.Template {
	funcs := map[string]interface{}{
		"contextType": contextType,
	}
	return template.Must(template.New("thrift-gen").Funcs(funcs).Parse(serviceTmpl))
}

func generateCode(outputFile string, tmpl *template.Template, pkg string, parsed *parser.Thrift) error {
	if outputFile == "" {
		return fmt.Errorf("must speciy an output file")
	}

	wrappedServices, err := wrapServices(parsed)
	if err != nil {
		log.Fatalf("Service parsing error: %v", err)
	}

	buf := &bytes.Buffer{}

	td := TemplateData{
		Package:        pkg,
		Services:       wrappedServices,
		ThriftImport:   *apacheThriftImport,
		TChannelImport: tchannelThriftImport,
	}
	if err := tmpl.Execute(buf, td); err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}

	generated := cleanGeneratedCode(buf.Bytes())
	if err := ioutil.WriteFile(outputFile, generated, 0666); err != nil {
		return fmt.Errorf("cannot write output file %s: %v", outputFile, err)
	}

	// Run gofmt on the file (ignore any errors)
	exec.Command("gofmt", "-w", outputFile).Run()
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

// find all the thrift files in the directory structure of root
// using $GOPATH when root is nil
func findThriftGenFiles(root string) ([]string, error) {
	if root == "" {
		root = os.Getenv("GOPATH")
	}

	var files []string
	// walk the root directory to find the *.thrift-gen files
	err := filepath.Walk(root, func(path string, f os.FileInfo, err error) error {
		if f.Name() == "Godeps" {
			// skip godeps
			return filepath.SkipDir
		}

		if filepath.Ext(path) == ".thrift-gen" {
			files = append(files, path)
		}
		return nil
	})

	// test if there is an error during walking the directory
	if err != nil {
		return nil, err
	}

	return files, nil
}

func getTemplateFromFile(filename string) (*template.Template, error) {
	funcs := map[string]interface{}{
		"contextType": contextType,
		"lowercase": func(in string) string {
			return strings.ToLower(in)
		},
		"goPublicName": goPublicName,
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

func getTemplatesFromFiles(files []string) (map[string]*template.Template, error) {
	var output map[string]*template.Template
	output = make(map[string]*template.Template)

	var templates []*template.Template
	for _, filename := range files {
		parsedTemplate, err := getTemplateFromFile(filename)
		if err != nil {
			// PANIC!
			return nil, err
		} else {
			// add parsed template to templates
			output[filename] = parsedTemplate
			templates = append(templates, parsedTemplate)
		}
	}
	return output, nil
}

func outputDirectory(infile string, AST *parser.Thrift) string {
	dir, _ := filepath.Split(infile)
	genDir := filepath.Join(dir, ".gen", "go")
	return genDir
}

func getOuputFilename(thriftFile string, AST *parser.Thrift, genFile string) string {
	dir := outputDirectory(thriftFile, AST)

	baseThriftFilename := getBasename(thriftFile)
	baseGenFilename := getBasename(genFile)

	return filepath.Join(dir, baseThriftFilename, baseThriftFilename+"-"+baseGenFilename+".go")
}

func getBasename(path string) string {
	_, filename := filepath.Split(path)
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}
