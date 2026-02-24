/*
Copyright 2026, Aleksei Sviridkin.

SPDX-License-Identifier: BSD-3-Clause
*/

package testutil

import (
	"archive/zip"
	"bytes"
)

// BuildTestJAR creates an in-memory JAR (ZIP) file with a single entry.
// Panics on error since this is a test utility.
func BuildTestJAR(filename, content string) []byte {
	return BuildTestJARMulti(map[string]string{filename: content})
}

// BuildTestJARMulti creates an in-memory JAR (ZIP) file with multiple entries.
// Panics on error since this is a test utility.
func BuildTestJARMulti(files map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			panic("BuildTestJARMulti: " + err.Error())
		}

		if _, err := f.Write([]byte(content)); err != nil {
			panic("BuildTestJARMulti: " + err.Error())
		}
	}

	if err := w.Close(); err != nil {
		panic("BuildTestJARMulti: " + err.Error())
	}

	return buf.Bytes()
}
