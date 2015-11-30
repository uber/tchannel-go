package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func ResolveWithGopath(filename string) (string, error) {
	// find the first candidate file that exists
	for _, file := range FileCandidatesWithGopath(filename) {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			return file, nil
		}
	}

	return "", errors.New(fmt.Sprintf("file not found on gopath: %s", filename))
}

func FileCandidatesWithGopath(filename string) []string {
	candidates := []string{filename}

	paths := filepath.SplitList(os.Getenv("GOPATH"))

	for _, path := range paths {
		resolvedFilename := filepath.Join(path, "src", filename)
		candidates = append(candidates, resolvedFilename)
	}

	return candidates
}
