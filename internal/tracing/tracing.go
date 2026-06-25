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

// Package tracing wires up OpenTelemetry tracing for Griffino.
//
// Tracing is OFF by default: Init only installs an OTLP exporter when an
// endpoint is configured. With no endpoint it leaves the global no-op tracer
// provider in place and returns a no-op shutdown, so the daemon never needs a
// running collector.
package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// noopShutdown is returned when tracing is disabled; calling it is safe and
// always succeeds.
func noopShutdown(context.Context) error { return nil }

// Init configures OpenTelemetry tracing.
//
// If otlpEndpoint is empty, tracing is left disabled: the global provider is
// untouched and a no-op shutdown is returned with no error. Otherwise an OTLP
// HTTP trace exporter is created, wired into an sdktrace.TracerProvider whose
// resource is named by serviceName, and installed as the global provider.
//
// The returned shutdown flushes and tears down the provider; it must be called
// on daemon exit (typically via defer).
func Init(ctx context.Context, otlpEndpoint, serviceName string) (func(context.Context) error, error) {
	if otlpEndpoint == "" {
		// Disabled: keep the default no-op provider, nothing to tear down.
		return noopShutdown, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(otlpEndpoint))
	if err != nil {
		return nil, err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		// Fall back to a resource carrying just the service name.
		res = resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
