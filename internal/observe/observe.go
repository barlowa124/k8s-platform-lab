// Prometheus-format metrics and OpenTelemetry tracing for previewctl.
// Metrics are emitted in the text exposition format by hand so the
// only external dependency is the OTel SDK itself.
package observe

import (
	"fmt"
	"net/http"
	"sort"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Metrics holds the counters and gauges previewctl exports.
type Metrics struct {
	mu               sync.Mutex
	ReconcilesTotal  int64
	ReconcileErrors  int64
	AppsManaged      int64
	ReadyReplicas    map[string]int64
	LastReconcileSec float64
}

func NewMetrics() *Metrics {
	return &Metrics{ReadyReplicas: map[string]int64{}}
}

// Record feeds one reconcile outcome into the counters.
func (m *Metrics) Record(app string, ready int64,
	secs float64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReconcilesTotal++
	if err != nil {
		m.ReconcileErrors++
	}
	m.ReadyReplicas[app] = ready
	m.LastReconcileSec = secs
}

// SetManaged updates the apps-managed gauge from the resync list.
func (m *Metrics) SetManaged(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AppsManaged = n
}

// Handler serves GET /metrics in Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w,
			"# HELP previewctl_reconciles_total Reconcile runs.\n"+
				"# TYPE previewctl_reconciles_total counter\n"+
				"previewctl_reconciles_total %d\n"+
				"# HELP previewctl_reconcile_errors_total Failed reconciles.\n"+
				"# TYPE previewctl_reconcile_errors_total counter\n"+
				"previewctl_reconcile_errors_total %d\n"+
				"# HELP previewctl_apps_managed PreviewApps under management.\n"+
				"# TYPE previewctl_apps_managed gauge\n"+
				"previewctl_apps_managed %d\n"+
				"# HELP previewctl_reconcile_seconds Most recent reconcile duration.\n"+
				"# TYPE previewctl_reconcile_seconds gauge\n"+
				"previewctl_reconcile_seconds %.4f\n"+
				"# HELP previewctl_ready_replicas Ready replicas per app.\n"+
				"# TYPE previewctl_ready_replicas gauge\n",
			m.ReconcilesTotal, m.ReconcileErrors, m.AppsManaged,
			m.LastReconcileSec)
		keys := make([]string, 0, len(m.ReadyReplicas))
		for k := range m.ReadyReplicas {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w,
				`previewctl_ready_replicas{app=%q} %d`+"\n",
				k, m.ReadyReplicas[k])
		}
	})
}

// Tracer configures OTel with a stdout exporter so reconcile spans
// land in the controller log as evidence.
func Tracer() (trace.Tracer, func(), error) {
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	return otel.Tracer("previewctl"),
		func() { _ = tp.Shutdown(nil) }, nil
}

// StartSpan wraps one reconcile in a span with app and outcome attrs.
func StartSpan(tr trace.Tracer, name string, app string) (
	trace.Span, func(error)) {
	_, span := tr.Start(nil, name,
		trace.WithAttributes(attribute.String("previewapp", app)))
	return span, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}
