/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
