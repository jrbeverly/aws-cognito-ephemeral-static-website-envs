// ==============================================================================
// metrics.go — Embedded Metric Format (EMF) emitter for the validation worker
//
// Emits structured JSON to stdout via os.Stdout.  When running in Lambda, the
// CloudWatch Agent extracts EMF blobs from the log stream and creates
// CloudWatch Metrics — no SDK dependencies required.
//
// Namespace: Sites/Platform (shared with the backend API).
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
// Namespace — shared with the backend API
// ==============================================================================

const Namespace = "Sites/Platform"

// ==============================================================================
// MetricDefinition — describes a single metric in an EMF blob
// ==============================================================================

type MetricDefinition struct {
	Name string `json:"Name"`
	Unit string `json:"Unit"`
}

// ==============================================================================
// Emitter — thread-safe EMF emitter backed by stdout
// ==============================================================================

type Emitter struct {
	mu sync.Mutex
}

func NewEmitter() *Emitter {
	return &Emitter{}
}

// ==============================================================================
// EmitValidationEvent — validation success/failure with reason
//
// Emits:
//   - ValidationSucceeded (Count) — when succeeded is true
//   - ValidationFailed (Count, dimension: FailureReason) — when succeeded is false
// ==============================================================================

func (e *Emitter) EmitValidationEvent(succeeded bool, reason string, uploadType string) {
	var metricName string
	if succeeded {
		metricName = "ValidationSucceeded"
	} else {
		metricName = "ValidationFailed"
	}

	dims := map[string]string{"UploadType": uploadType}
	dimSets := [][]string{{"UploadType"}}

	if !succeeded && reason != "" {
		dims["FailureReason"] = reason
		dimSets = append(dimSets, []string{"FailureReason"})
	}

	e.emit(
		Namespace,
		dimSets,
		[]MetricDefinition{{Name: metricName, Unit: "Count"}},
		dims,
		map[string]float64{metricName: 1},
	)
}

// ==============================================================================
// EmitPublishEvent — publish success/failure
//
// Emits:
//   - PublishSucceeded (Count)
//   - PublishFailed (Count, dimension: FailureReason)
// ==============================================================================

func (e *Emitter) EmitPublishEvent(succeeded bool, reason string) {
	var metricName string
	if succeeded {
		metricName = "PublishSucceeded"
	} else {
		metricName = "PublishFailed"
	}

	dimSets := [][]string{{}}
	dims := map[string]string{}

	if !succeeded && reason != "" {
		dimSets = [][]string{{"FailureReason"}}
		dims["FailureReason"] = reason
	}

	e.emit(
		Namespace,
		dimSets,
		[]MetricDefinition{{Name: metricName, Unit: "Count"}},
		dims,
		map[string]float64{metricName: 1},
	)
}

// ==============================================================================
// EmitWorkerHeartbeat — periodic heartbeat to confirm the worker is alive
//
// Emits:
//   - WorkerHeartbeat (Count)
// ==============================================================================

func (e *Emitter) EmitWorkerHeartbeat() {
	e.emit(
		Namespace,
		[][]string{{}},
		[]MetricDefinition{{Name: "WorkerHeartbeat", Unit: "Count"}},
		map[string]string{},
		map[string]float64{"WorkerHeartbeat": 1},
	)
}

// ==============================================================================
// internal emit — serialize and write the EMF blob to stdout
// ==============================================================================

func (e *Emitter) emit(namespace string, dims [][]string, metrics []MetricDefinition, dimValues map[string]string, metricValues map[string]float64) {
	blob := map[string]any{
		"_aws": map[string]any{
			"Timestamp": time.Now().UnixMilli(),
			"CloudWatchMetrics": []map[string]any{
				{
					"Namespace":  namespace,
					"Dimensions": dims,
					"Metrics":    metrics,
				},
			},
		},
	}
	for k, v := range dimValues {
		blob[k] = v
	}
	for k, v := range metricValues {
		blob[k] = v
	}

	b, err := json.Marshal(blob)
	if err != nil {
		slog.Error("failed to marshal EMF blob", "error", err)
		return
	}

	e.mu.Lock()
	os.Stdout.Write(b)
	os.Stdout.Write([]byte{'\n'})
	e.mu.Unlock()
}
