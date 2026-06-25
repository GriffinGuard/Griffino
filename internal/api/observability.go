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

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/GriffinGuard/Griffino/internal/metrics"
)

// statusRecorder wraps http.ResponseWriter to capture the status code so the
// metrics middleware can label by it. Defaults to 200 when WriteHeader is
// never called explicitly.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// metricsMiddleware records griffino_http_* metrics for every request. It
// labels by method and status code only — never by URL path, which carries
// plugin IDs / usernames and would blow up label cardinality.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		metrics.ObserveHTTP(r.Method, strconv.Itoa(rec.status), time.Since(start).Seconds())
	})
}
