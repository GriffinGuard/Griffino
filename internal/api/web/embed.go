// Copyright 2025 GriffinGuard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package web embeds the prebuilt Griffino Web UI and serves it as a
// single-page app. The built assets live under dist/ and are produced by the
// separate Griffino-WebUI repo; scripts/fetch-webui.sh stages them at build
// time. A committed placeholder index.html keeps `go build` working before the
// first UI release exists.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded single-page app. Requests that map to an existing
// static asset are served directly; everything else falls back to index.html so
// the client-side router can take over.
func Handler() (http.Handler, error) {
	root, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" {
			if f, err := root.Open(name); err == nil {
				info, statErr := f.Stat()
				_ = f.Close()
				if statErr == nil && !info.IsDir() {
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}
		// SPA fallback: serve index.html for unknown paths (client-side routes).
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	}), nil
}
