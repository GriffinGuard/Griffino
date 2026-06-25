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

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndexAtRoot(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error: %v", err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rr.Body.String(), "<html") {
		t.Errorf("body does not look like HTML: %q", rr.Body.String())
	}
}

func TestHandlerSPAFallback(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error: %v", err)
	}
	// An unknown client-side route must fall back to index.html, not 404.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/plugins/some-id/config", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("SPA route status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<html") {
		t.Errorf("SPA fallback did not return index.html")
	}
}

func TestHandlerRoutesExistingFileToFileServer(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error: %v", err)
	}
	// An existing file goes through http.FileServer, which canonicalizes
	// /index.html to / with a 301. Getting the redirect (rather than a 200
	// HTML body) confirms the static path was taken instead of the SPA fallback.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/index.html", nil))

	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /index.html status = %d, want 301 (FileServer canonicalization)", rr.Code)
	}
}
