// ==============================================================================
// metrics.go — Embedded Metric Format (EMF) emitter for CloudWatch Metrics
//
// Emits structured JSON to stdout via slog.  When running in Lambda, the
// CloudWatch Agent automatically extracts EMF blobs from the log stream and
// creates CloudWatch Metrics — no PutMetricData API calls, no SDK deps.
//
// Reference:
//
//	https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Specification.html
//
// VISION.md §14 — Observability requirements.
// ==============================================================================

package metrics

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// ==============================================================================
// Namespace — all platform metrics share one namespace
// ==============================================================================

const Namespace = "Sites/Platform"

// ==============================================================================
// MetricDefinition — describes a single metric in an EMF blob
// ==============================================================================

// MetricDefinition describes a single CloudWatch metric emitted in an EMF blob.
// Name becomes the metric name.  Unit must be a valid CloudWatch unit:
// Count, Milliseconds, Bytes, Percent, etc.
type MetricDefinition struct {
	Name string `json:"Name"`
	Unit string `json:"Unit"`
}

// ==============================================================================
// EMF blob — the JSON shape emitted to stdout
//
// The _aws field is the EMF metadata envelope.  Additional fields carry the
// metric values and dimensions — CloudWatch uses the metadata to extract them.
// ==============================================================================

// emfBlob is the root object for an EMF log line.
// Metric values are set as direct fields on the struct via MarshalJSON
// (see Emitter.Emit).
type emfBlob struct {
	AWS emfMetadata `json:"_aws"`
	// Additional metric/dimension fields are merged at emit time.
}

// emfMetadata carries the CloudWatch target namespace, dimensions, and metrics.
type emfMetadata struct {
	Timestamp           int64              `json:"Timestamp"`
	CloudWatchMetrics   []emfMetricGroup   `json:"CloudWatchMetrics"`
}

type emfMetricGroup struct {
	Namespace  string             `json:"Namespace"`
	Dimensions [][]string         `json:"Dimensions"`
	Metrics    []MetricDefinition `json:"Metrics"`
}

// ==============================================================================
// Emitter — thread-safe EMF emitter backed by slog
// ==============================================================================

// Emitter writes EMF-formatted JSON to CloudWatch Logs via slog.  It is safe
// for concurrent use from multiple goroutines.
//
// Usage:
//
//	emitter := metrics.NewEmitter()
//	emitter.EmitHTTPMetrics("GET /api/sites", 200, 12*time.Millisecond)
type Emitter struct {
	mu sync.Mutex
}

// NewEmitter creates an Emitter that writes to os.Stdout via slog.
func NewEmitter() *Emitter {
	return &Emitter{}
}

// ==============================================================================
// EmitHTTPMetrics — per-request HTTP metrics
//
// Captures one request's outcome.  Emitted as a single EMF blob with:
//   - RequestCount (Count, dimension: Operation)
//   - LatencyMs (Milliseconds, dimension: Operation)
//   - ErrorCount (Count, dimension: Operation) — when status >= 400
//   - FaultCount (Count, dimension: Operation) — when status >= 500
// ==============================================================================

// EmitHTTPMetrics records standard HTTP service metrics for a single request.
// operation describes the endpoint, e.g. "POST /api/sites".
// statusCode is the HTTP response status.  Latency is the wall-clock duration.
func (e *Emitter) EmitHTTPMetrics(operation string, statusCode int, latency time.Duration) {
	metrics := []MetricDefinition{
		{Name: "RequestCount", Unit: "Count"},
		{Name: "LatencyMs", Unit: "Milliseconds"},
	}
	values := map[string]float64{
		"RequestCount": 1,
		"LatencyMs":    float64(latency.Milliseconds()),
	}

	if statusCode >= 400 {
		metrics = append(metrics, MetricDefinition{Name: "ErrorCount", Unit: "Count"})
		values["ErrorCount"] = 1
	}
	if statusCode >= 500 {
		metrics = append(metrics, MetricDefinition{Name: "FaultCount", Unit: "Count"})
		values["FaultCount"] = 1
	}

	e.emit(emfMetricGroup{
		Namespace:  Namespace,
		Dimensions: [][]string{{"Operation"}},
		Metrics:    metrics,
	}, map[string]string{
		"Operation": operation,
	}, values)
}

// ==============================================================================
// EmitUploadEvent — upload lifecycle event
//
// Captures upload grant, completion, and failure events.
//   - UploadRequested   (Count, dimension: UploadType)
//   - UploadCompleted   (Count, dimension: UploadType)
//   - UploadFailed      (Count, dimension: UploadType, FailureReason)
// ==============================================================================

// EmitUploadEvent records an upload lifecycle metric.
// event must be one of: "requested", "completed", "failed".
// uploadType is "zip", "index", or "paste".
// failureReason is only relevant when event == "failed"; empty otherwise.
func (e *Emitter) EmitUploadEvent(event, uploadType, failureReason string) {
	var metricName string
	switch event {
	case "requested":
		metricName = "UploadRequested"
	case "completed":
		metricName = "UploadCompleted"
	case "failed":
		metricName = "UploadFailed"
	default:
		return
	}

	dims := [][]string{{"UploadType"}}
	dimValues := map[string]string{"UploadType": uploadType}

	if failureReason != "" {
		dims = append(dims, []string{"FailureReason"})
		dimValues["FailureReason"] = failureReason
	}

	e.emit(emfMetricGroup{
		Namespace:  Namespace,
		Dimensions: dims,
		Metrics:    []MetricDefinition{{Name: metricName, Unit: "Count"}},
	}, dimValues, map[string]float64{metricName: 1})
}

// ==============================================================================
// EmitPublishEvent — publish success/failure
//
// Captures the outcome of the publish step (after validation).
//   - PublishSucceeded (Count)
//   - PublishFailed    (Count, dimension: FailureReason)
// ==============================================================================

// EmitPublishEvent records a publish outcome metric.
// succeeded: true for successful publish, false for failure.
// reason: the failure reason when succeeded is false (validation error, etc.).
func (e *Emitter) EmitPublishEvent(succeeded bool, reason string) {
	var metricName string
	if succeeded {
		metricName = "PublishSucceeded"
	} else {
		metricName = "PublishFailed"
	}

	dims := [][]string{{}}
	dimValues := map[string]string{}

	if !succeeded && reason != "" {
		dims = [][]string{{"FailureReason"}}
		dimValues["FailureReason"] = reason
	}

	e.emit(emfMetricGroup{
		Namespace:  Namespace,
		Dimensions: dims,
		Metrics:    []MetricDefinition{{Name: metricName, Unit: "Count"}},
	}, dimValues, map[string]float64{metricName: 1})
}

// ==============================================================================
// EmitValidationEvent — validation success/failure with reason
//
// Captures validation outcomes from the worker.
//   - ValidationSucceeded (Count)
//   - ValidationFailed    (Count, dimension: FailureReason)
// ==============================================================================

// EmitValidationEvent records a validation outcome metric.
func (e *Emitter) EmitValidationEvent(succeeded bool, reason string) {
	var metricName string
	if succeeded {
		metricName = "ValidationSucceeded"
	} else {
		metricName = "ValidationFailed"
	}

	dims := [][]string{{}}
	dimValues := map[string]string{}

	if !succeeded && reason != "" {
		dims = [][]string{{"FailureReason"}}
		dimValues["FailureReason"] = reason
	}

	e.emit(emfMetricGroup{
		Namespace:  Namespace,
		Dimensions: dims,
		Metrics:    []MetricDefinition{{Name: metricName, Unit: "Count"}},
	}, dimValues, map[string]float64{metricName: 1})
}

// ==============================================================================
// internal emit — serialize and write the EMF blob via slog
// ==============================================================================

func (e *Emitter) emit(group emfMetricGroup, dims map[string]string, values map[string]float64) {
	blob := emfBlob{
		AWS: emfMetadata{
			Timestamp:         time.Now().UnixMilli(),
			CloudWatchMetrics: []emfMetricGroup{group},
		},
	}

	// Build the raw map — slog can't do dynamic keys, so we marshal directly
	// and write as a raw JSON line.
	raw := make(map[string]any, 2+len(dims)+len(values))
	raw["_aws"] = blob.AWS
	for k, v := range dims {
		raw[k] = v
	}
	for k, v := range values {
		raw[k] = v
	}

	b, err := json.Marshal(raw)
	if err != nil {
		slog.Error("failed to marshal EMF blob", "error", err)
		return
	}

	// Write directly to stdout to avoid double-JSON-encoding through slog.
	e.mu.Lock()
	os.Stdout.Write(b)
	os.Stdout.Write([]byte{'\n'})
	e.mu.Unlock()
}
