// Copyright (c) 2019 The Jaeger Authors.
// Copyright (c) 2017 Uber Technologies, Inc.
// SPDX-License-Identifier: Apache-2.0

package tracing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"go.uber.org/zap"

	ddotel "github.com/DataDog/dd-trace-go/v2/ddtrace/opentelemetry"
	ddtracer "github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"

	"github.com/jaegertracing/jaeger/examples/hotrod/pkg/log"
	"github.com/jaegertracing/jaeger/examples/hotrod/pkg/tracing/rpcmetrics"
	"github.com/jaegertracing/jaeger/internal/metrics"
	"github.com/jaegertracing/jaeger/internal/telemetry/otelsemconv"
)

var once sync.Once

// InitOTEL initializes OpenTelemetry SDK.
func InitOTEL(serviceName string, exporterType string, metricsFactory metrics.Factory, logger log.Factory) trace.TracerProvider {
	once.Do(func() {
		otel.SetTextMapPropagator(
			propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			),
		)
	})

	// Datadog dd-trace-go: quando exporterType == "datadog", os spans criados pela API
	// OpenTelemetry (otelhttp, otelgrpc e spans manuais do HotROD) passam a ser
	// gerados pelo tracer nativo da Datadog (dd-trace-go) e enviados ao Datadog Agent.
	// Configuração via env: DD_SERVICE, DD_ENV, DD_VERSION, DD_AGENT_HOST /
	// DD_TRACE_AGENT_URL. Um processo = um serviço (rodar cada subcomando em seu pod).
	if exporterType == "datadog" {
		return initDatadog(serviceName, logger)
	}

	exp, err := createOtelExporter(exporterType)
	if err != nil {
		logger.Bg().Fatal("cannot create exporter", zap.String("exporterType", exporterType), zap.Error(err))
	}
	logger.Bg().Debug("using " + exporterType + " trace exporter")

	rpcmetricsObserver := rpcmetrics.NewObserver(metricsFactory, rpcmetrics.DefaultNameNormalizer)

	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(otelsemconv.ServiceNameAttribute(serviceName)),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOSType(),
	)
	if err != nil {
		logger.Bg().Fatal("resource creation failed", zap.Error(err))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(1000*time.Millisecond)),
		sdktrace.WithSpanProcessor(rpcmetricsObserver),
		sdktrace.WithResource(res),
	)
	logger.Bg().Debug("Created OTEL tracer", zap.String("service-name", serviceName))
	return tp
}

var (
	ddOnce       sync.Once
	ddProvider   trace.TracerProvider
	ddMainSvc    string
	ddMainSvcMux sync.Mutex
)

// initDatadog inicia o tracer dd-trace-go uma única vez por processo (o tracer
// Datadog é global). O primeiro serviço inicializado é o serviço principal do
// processo (DD_SERVICE, se definido, tem prioridade). Serviços adicionais no
// mesmo processo (os "bancos" simulados mysql / redis-manual) recebem o
// atributo service.name em cada span, que o dd-trace-go mapeia para o serviço
// Datadog — assim aparecem como serviços próprios, como no Jaeger.
func initDatadog(serviceName string, logger log.Factory) trace.TracerProvider {
	ddOnce.Do(func() {
		opts := []ddtracer.StartOption{
			ddtracer.WithRuntimeMetrics(),
			ddtracer.WithLogStartup(true),
		}
		if os.Getenv("DD_SERVICE") == "" {
			opts = append(opts, ddtracer.WithService(serviceName))
		}
		ddProvider = ddotel.NewTracerProvider(opts...)
		ddMainSvcMux.Lock()
		ddMainSvc = serviceName
		ddMainSvcMux.Unlock()
		logger.Bg().Info("using Datadog dd-trace-go tracer provider", zap.String("service-name", serviceName))
	})
	ddMainSvcMux.Lock()
	main := ddMainSvc
	ddMainSvcMux.Unlock()
	if serviceName == main {
		return ddProvider
	}
	svc := serviceName
	if prefix := os.Getenv("DD_SERVICE_PREFIX"); prefix != "" {
		svc = prefix + serviceName
	}
	logger.Bg().Info("Datadog: secondary service in this process", zap.String("service-name", svc))
	return &serviceTracerProvider{tp: ddProvider, service: svc}
}

// serviceTracerProvider força o serviço Datadog (service.name) de todos os spans.
type serviceTracerProvider struct {
	embedded.TracerProvider
	tp      trace.TracerProvider
	service string
}

func (p *serviceTracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return &serviceTracer{t: p.tp.Tracer(name, opts...), service: p.service}
}

type serviceTracer struct {
	embedded.Tracer
	t       trace.Tracer
	service string
}

func (t *serviceTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append(opts, trace.WithAttributes(attribute.String("service.name", t.service)))
	return t.t.Start(ctx, name, opts...)
}

// withSecure instructs the client to use HTTPS scheme, instead of hotrod's desired default HTTP
func withSecure() bool {
	return strings.HasPrefix(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "https://") ||
		strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")) == "false"
}

func createOtelExporter(exporterType string) (sdktrace.SpanExporter, error) {
	var exporter sdktrace.SpanExporter
	var err error
	switch exporterType {
	case "jaeger":
		return nil, errors.New("jaeger exporter is no longer supported, please use otlp")
	case "otlp":
		var opts []otlptracehttp.Option
		if !withSecure() {
			opts = []otlptracehttp.Option{otlptracehttp.WithInsecure()}
		}
		exporter, err = otlptrace.New(
			context.Background(),
			otlptracehttp.NewClient(opts...),
		)
	case "stdout":
		exporter, err = stdouttrace.New()
	default:
		return nil, fmt.Errorf("unrecognized exporter type %s", exporterType)
	}
	return exporter, err
}
