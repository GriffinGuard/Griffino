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

package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerExposesRecordedMetrics(t *testing.T) {
	// Record a few samples across every metric family.
	ObserveHTTP("GET", "200", 0.012)
	RouterMessage("ok")
	RouterMessage("failover")
	ObserveRouterMessage(0.5)
	TaskRun("ok", 1.5)
	TaskRun("timeout", 2.0)
	HealthTransition("failed")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d, want %d", rec.Code, http.StatusOK)
	}

	bodyBytes, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(bodyBytes)

	wants := []string{
		`griffino_http_requests_total{method="GET",status="200"}`,
		`griffino_http_request_duration_seconds_bucket{method="GET"`,
		`griffino_router_messages_total{result="ok"}`,
		`griffino_router_messages_total{result="failover"}`,
		`griffino_router_message_duration_seconds_bucket`,
		`griffino_task_runs_total{result="ok"}`,
		`griffino_task_runs_total{result="timeout"}`,
		`griffino_task_run_duration_seconds_bucket`,
		`griffino_plugin_health_transitions_total{to_state="failed"}`,
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q\n---\n%s", want, body)
		}
	}
}
