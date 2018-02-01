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

// vbumper helps bump version numbers in the repository and in the CHANGELOG.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	_changelogFile = flag.String("changelog-file", "CHANGELOG.md", "Filename of the changelog file")
	_versionFile   = flag.String("version-file", "version.go", "Filename of where the version information is stored")
	_version       = flag.String("version", "", "Version to mention in changelog and version.go")
	_versionDate   = flag.String("version-date", "", "Date to use in the changelog, by default the current date")
	_skipChangelog = flag.Bool("skip-changelog", false, "Skip updating the changelog")
)

func main() {
	*_versionDate = time.Now().Format("2006-01-02")
	flag.Parse()

	if *_version == "" {
		log.Fatal("Please specify the version to release using --version")
	}
	*_version = strings.TrimPrefix(*_version, "v")

	prevVersion, err := updateChangelog()
	if err != nil {
		log.Fatal("failed to update changelog", err)
	}

	if err := updateVersion(prevVersion); err != nil {
		log.Fatal("failed to update version", err)
	}
}

func updateVersion(prevVersion string) error {
	versionBytes, err := ioutil.ReadFile(*_versionFile)
	if err != nil {
		return err
	}

	newContents := insertNewVersion(string(versionBytes), prevVersion, *_version)
	return ioutil.WriteFile(*_versionFile, []byte(newContents), 0666)
}

func insertNewVersion(contents, prevVersion, newVersion string) string {
	// Find the version string in the file
	versionStart := strings.Index(contents, prevVersion)
	versionLine := contents[versionStart:]
	versionEnd := strings.Index(versionLine, `"`) + versionStart
	return contents[:versionStart] + newVersion + contents[versionEnd:]
}

func updateChangelog() (oldVersion string, _ error) {
	changelogBytes, err := ioutil.ReadFile(*_changelogFile)
	if err != nil {
		return "", err
	}

	newLog, oldVersion, err := insertNewChangelog(string(changelogBytes))
	if err != nil {
		return "", err
	}

	newLog, err = insertChangesLink(newLog, oldVersion, *_version)
	if err != nil {
		return "", err
	}

	if *_skipChangelog {
		return oldVersion, nil
	}

	return oldVersion, ioutil.WriteFile(*_changelogFile, []byte(newLog), 0666)
}

func insertNewChangelog(contents string) (string, string, error) {
	prevVersionHeader := strings.Index(contents, "\n## [")
	if prevVersionHeader < 0 {
		return "", "", errors.New("failed to find version header in changelog")
	}

	// Skip the newline
	prevVersionHeader++
	versionLine := contents[prevVersionHeader:]
	prevVersionEnd := strings.Index(versionLine, "]")
	prevVersion := strings.TrimSpace(versionLine[4:prevVersionEnd])

	// The version tag has a "v" prefix.
	newChanges, err := getNewChangelog("v" + prevVersion)
	if err != nil {
		return "", "", err
	}

	newContents := contents[:prevVersionHeader] + newChanges + contents[prevVersionHeader:]
	return newContents, prevVersion, nil
}

func getNewChangelog(prevVersion string) (string, error) {
	changes, err := getChanges(prevVersion)
	if err != nil {
		return "", err
	}

	if len(changes) == 0 {
		changes = []string{"No changes yet"}
	}

	buf := &bytes.Buffer{}
	_changeTmpl.Execute(buf, struct {
		Version string
		Date    string
		Changes []string
	}{
		Version: *_version,
		Date:    *_versionDate,
		Changes: changes,
	})
	return buf.String(), nil
}

var _changeTmpl = template.Must(template.New("changelog").Parse(
	`## [{{ .Version }}] - {{ .Date }}
### Changed
{{ range .Changes }}
 * {{ . -}}
{{ end }}

`))

func getChanges(prevVersion string) ([]string, error) {
	cmd := exec.Command("git", "log", "--format=%s", "--no-merges", prevVersion+"..HEAD")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(out), "\n")
	newLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		newLines = append(newLines, line)
	}
	return newLines, nil
}

func insertChangesLink(contents, prevVersion, version string) (string, error) {
	linksMarker := strings.Index(contents, "(Version Links)")
	if linksMarker == -1 {
		return "", errors.New("failed to find marker for version links section")
	}

	newLine := strings.IndexByte(contents[linksMarker:], '\n')
	if newLine < 0 {
		return "", errors.New("failed to find newline after version links section")
	}

	insertAt := linksMarker + newLine + 1

	linkBlock := fmt.Sprintf("[%v]: %v\n", version, getChangesLink(prevVersion, version))
	newContents := contents[:insertAt] + linkBlock + contents[insertAt:]
	return newContents, nil
}

func getChangesLink(prevVersion, curVersion string) string {
	// Example link:
	// https://github.com/uber/tchannel-go/compare/v1.8.0...v1.8.1
	return fmt.Sprintf("https://github.com/uber/tchannel-go/compare/v%v...v%v", prevVersion, curVersion)
}
