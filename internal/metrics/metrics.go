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

// Package metrics centralizes Prometheus instrumentation for Griffino.
//
// Collectors are registered on a private registry and exposed through
// Handler(). Other packages record samples via the exported helper funcs
// (ObserveHTTP, RouterMessage, TaskRun, …) so they never import the
// prometheus client directly. Label cardinality is deliberately bounded:
// no raw URL path (which carries plugin IDs / usernames) is ever used as a
// label value.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry = prometheus.NewRegistry()

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "griffino_http_requests_total",
			Help: "Total HTTP requests handled by the API server.",
		},
		[]string{"method", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "griffino_http_request_duration_seconds",
			Help:    "HTTP request handling duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method"},
	)

	routerMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "griffino_router_messages_total",
			Help: "Total router messages handled, by outcome (ok|failover|dropped|error).",
		},
		[]string{"result"},
	)

	routerMessageDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "griffino_router_message_duration_seconds",
			Help:    "Router message handling duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)

	taskRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "griffino_task_runs_total",
			Help: "Total task runs completed, by outcome (ok|error|timeout).",
		},
		[]string{"result"},
	)

	taskRunDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "griffino_task_run_duration_seconds",
			Help:    "Task run duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)

	pluginHealthTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "griffino_plugin_health_transitions_total",
			Help: "Total plugin health state transitions, by destination state.",
		},
		[]string{"to_state"},
	)
)

func init() {
	registry.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		routerMessagesTotal,
		routerMessageDuration,
		taskRunsTotal,
		taskRunDuration,
		pluginHealthTransitionsTotal,
	)
}

// Handler returns an http.Handler that serves the Prometheus exposition
// format for Griffino's registry. Mount it on GET /metrics.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// ObserveHTTP records one HTTP request: a counter keyed by method+status and
// a duration histogram keyed by method. status is the textual status code
// (e.g. "200"); dur is in seconds.
func ObserveHTTP(method, status string, dur float64) {
	httpRequestsTotal.WithLabelValues(method, status).Inc()
	httpRequestDuration.WithLabelValues(method).Observe(dur)
}

// RouterMessage increments the router message counter for the given result.
// result is one of: ok, failover, dropped, error.
func RouterMessage(result string) {
	routerMessagesTotal.WithLabelValues(result).Inc()
}

// ObserveRouterMessage records router message handling duration in seconds.
func ObserveRouterMessage(dur float64) {
	routerMessageDuration.Observe(dur)
}

// TaskRun records a finished task run: a counter keyed by result plus the
// run duration histogram. result is one of: ok, error, timeout; dur is in
// seconds.
func TaskRun(result string, dur float64) {
	taskRunsTotal.WithLabelValues(result).Inc()
	taskRunDuration.Observe(dur)
}

// HealthTransition increments the plugin health transition counter for the
// destination state (e.g. "running", "failed").
func HealthTransition(toState string) {
	pluginHealthTransitionsTotal.WithLabelValues(toState).Inc()
}
