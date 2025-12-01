// ==============================================================================
// main.go — Validation & publishing worker Lambda entry point
//
// The worker is invoked when an upload transitions to "uploaded" status.  It:
//   1. Reads the upload record from DynamoDB (via the upload ID)
//   2. Fetches the staged content from S3
//   3. Validates the content using the validate package
//   4. On success: publishes files to an immutable versioned S3 prefix and
//      atomically updates the active version pointer
//   5. On failure: records the validation errors on the upload record
//
// This entry point wires the full validation-to-publish flow (VISION.md §8.5).
// In production it runs as an AWS Lambda function triggered by DynamoDB Streams
// or S3 event notifications.  The Lambda runtime interface is handled by
// cmd/api/main.go's pattern — this file is the same shape but for the worker.
//
// For now, this is a compile target that exercises the full dependency graph.
// The Lambda event handler will be filled in as part of the triggering story.
//
// VISION.md §5.4 — Failure messages; §8.5 — Validation & publishing worker.
// VISION.md §14   — Observability: structured logs and metrics.
// VISION.md §18.7 — Prefer versioned publishing over in-place mutation.
// ==============================================================================

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"workers/site-publisher/internal/metrics"
	"workers/site-publisher/internal/publish"
	"workers/site-publisher/internal/validate"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// When running in Lambda, the runtime API loop would start here.
	// For now, the binary exits immediately — the validation and publish
	// logic lives in internal/ and is exercised via unit tests.
	if runningInLambda() {
		slog.Info("worker starting in Lambda mode (event handler not yet wired)")
	} else {
		slog.Info("worker built successfully (validation + publish packages available)")
	}
}

// runningInLambda returns true when the AWS Lambda Runtime API env var is set.
func runningInLambda() bool {
	return os.Getenv("AWS_LAMBDA_RUNTIME_API") != ""
}

// compile-time reference to the validate package so `go build` verifies it.
var _ = validate.DefaultConfig

// compile-time reference to the metrics package.
var _ = metrics.NewEmitter

// compile-time references to the publish package — verifies types and
// constructors compile correctly.
var _ = publish.NewVersionID
var _ = publish.NewPublisher

// ==============================================================================
// processUpload — full validation-to-publish flow, called per Lambda event
//
// In production, the Lambda handler reads the event from DynamoDB Streams or
// SQS, fetches the upload record from DynamoDB, reads staged content from S3,
// and calls this function.  The function:
//
//  1. Validates the staged content against platform thresholds.
//  2. On success: publishes normalized files to an immutable versioned S3
//     prefix via the publish.Publisher.
//  3. On failure: records the validation errors so the frontend can display them.
//
// Metrics are emitted for both validation and publish outcomes.
// ==============================================================================

// processUpload runs the validation step and returns the result together with
// context needed for publishing.  The caller is responsible for wiring the
// publish.Publisher with production S3/DynamoDB adapters.
//
// When validation fails, the returned error describes the failure and the
// caller should record it on the upload record via UploadStore.
//
// When validation succeeds, ValidatedFiles is populated and the caller
// should pass it to Publisher.Publish along with the upload ID.
func processUpload(uploadType string, content []byte, compressedSize int64) (*validate.Result, error) {
	cfg := validate.DefaultConfig()
	input := validate.Input{
		UploadType:          uploadType,
		Content:             content,
		CompressedSizeBytes: compressedSize,
	}
	result := validate.Validate(input, cfg)

	emitter := metrics.NewEmitter()

	if !result.Valid {
		// Emit validation failure metric with the first error as the reason.
		reason := "unknown validation error"
		if len(result.Errors) > 0 {
			reason = result.Errors[0]
		}
		emitter.EmitValidationEvent(false, reason, uploadType)
		emitter.EmitPublishEvent(false, reason)
		slog.Error("validation failed",
			"upload_type", uploadType,
			"errors", strings.Join(result.Errors, "; "),
		)
		return result, fmt.Errorf("validation failed: %v", result.Errors)
	}

	// Validation succeeded — emit metrics and return the validated files
	// so the caller can pass them to the publisher.
	emitter.EmitValidationEvent(true, "", uploadType)
	slog.Info("validation succeeded",
		"upload_type", uploadType,
		"file_count", len(result.ValidatedFiles),
	)

	return result, nil
}

// publishUpload is the second half of the flow.  It copies validated files to
// the versioned S3 prefix and atomically advances the active version pointer.
//
// The caller wires the Publisher with production adapters (S3 client,
// DynamoDB repositories).  This separation lets tests exercise validation
// and publishing independently.
//
// When publishUpload succeeds, Publisher.Publish has already written
// "published" status to the upload record.  On failure, the caller is
// responsible for recording the failure via Publisher.RecordValidationResult.
func publishUpload(ctx context.Context, pub *publish.Publisher, uploadID string, validatedFiles map[string][]byte) error {
	emitter := metrics.NewEmitter()

	pubResult, err := pub.Publish(ctx, publish.PublishInput{
		UploadID:       uploadID,
		ValidatedFiles: validatedFiles,
		FileCount:      len(validatedFiles),
		TotalBytes:     totalBytes(validatedFiles),
	})
	if err != nil {
		emitter.EmitPublishEvent(false, err.Error())
		slog.Error("publish failed",
			"upload_id", uploadID,
			"error", err,
		)
		return fmt.Errorf("publish failed: %w", err)
	}

	emitter.EmitPublishEvent(true, "")
	slog.Info("publish succeeded",
		"upload_id", uploadID,
		"version_id", pubResult.VersionID,
		"s3_prefix", pubResult.S3PublishedPrefix,
		"files_written", pubResult.FilesWritten,
	)

	return nil
}

// ==============================================================================
// HandleUpload — Full upload processing flow with result recording
//
// HandleUpload orchestrates the complete validate-then-publish-or-fail flow
// for a single upload.  It is the primary entry point called by the Lambda
// event handler after the staged content has been fetched from S3.
//
// Flow:
//  1. Look up the upload record to obtain the userID (needed for DynamoDB keys).
//  2. Validate the staged content against platform thresholds.
//  3. On validation failure: record the failure on the upload record and
//     return.  The active published site is NEVER modified.
//  4. On validation success: publish normalized files to the immutable
//     versioned S3 prefix and atomically advance the active version pointer.
//  5. On publish failure: record the failure on the upload record.
//
// VISION.md §5.3  — Publish result (success path)
// VISION.md §5.4  — Failure result (actionable messages on the upload record)
// VISION.md §8.5  — Validation & publishing worker
// VISION.md §14   — Observability: structured logs and metrics
// VISION.md §17.3 — Failed validation does not update the active site
// ==============================================================================

// HandleUploadResult describes the outcome of the full upload processing flow.
type HandleUploadResult struct {
	UploadID         string   // The upload that was processed
	Valid            bool     // Whether validation and publish succeeded
	Errors           []string // Failure reasons (empty on success)
	VersionID        string   // Newly published version ID (empty on failure)
	FilesPublished   int      // Number of files published (0 on failure)
}

// HandleUpload processes a single upload through the full validate-publish
// pipeline.  It records the outcome on the upload record so the frontend
// can surface it via GET /api/uploads/{id}.
//
// The caller must have already fetched the staged content from S3.  This
// function only handles validation and publishing — S3 downloads and event
// parsing live in the Lambda handler layer.
//
// On any failure (validation or publish), the active published site is
// NOT modified.  Only the upload record's status and validation result
// are updated to reflect the failure.
func HandleUpload(ctx context.Context, pub *publish.Publisher, uploadID string, uploadType string, content []byte, compressedSize int64) HandleUploadResult {
	// --------------------------------------------------------------------------
	// Step 1 — Look up the upload record to obtain the userID.
	// --------------------------------------------------------------------------
	upload, err := pub.LookupUploadByID(ctx, uploadID)
	if err != nil {
		slog.Error("handle upload: lookup failed",
			"upload_id", uploadID,
			"error", err,
		)
		return HandleUploadResult{
			UploadID: uploadID,
			Valid:    false,
			Errors:   []string{fmt.Sprintf("failed to look up upload: %v", err)},
		}
	}
	if upload == nil {
		slog.Error("handle upload: upload not found", "upload_id", uploadID)
		return HandleUploadResult{
			UploadID: uploadID,
			Valid:    false,
			Errors:   []string{"upload not found"},
		}
	}

	// --------------------------------------------------------------------------
	// Step 2 — Validate the staged content.
	// --------------------------------------------------------------------------
	cfg := validate.DefaultConfig()
	result := validate.Validate(validate.Input{
		UploadType:          uploadType,
		Content:             content,
		CompressedSizeBytes: compressedSize,
	}, cfg)

	emitter := metrics.NewEmitter()

	if !result.Valid {
		// ----------------------------------------------------------------------
		// Validation failed — record the failure and stop.
		//
		// The active published site is NOT modified.  Only the upload record
		// is updated so the frontend can display the failure reason.
		// ----------------------------------------------------------------------
		reason := "unknown validation error"
		if len(result.Errors) > 0 {
			reason = result.Errors[0]
		}
		emitter.EmitValidationEvent(false, reason, uploadType)
		emitter.EmitPublishEvent(false, reason)

		slog.Error("handle upload: validation failed",
			"upload_id", uploadID,
			"upload_type", uploadType,
			"errors", strings.Join(result.Errors, "; "),
		)

		vr := &publish.ValidationResult{
			Valid:  false,
			Errors: result.Errors,
		}
		if recordErr := pub.RecordValidationResult(ctx, upload.UserID, uploadID, vr); recordErr != nil {
			slog.Error("handle upload: failed to record validation failure",
				"upload_id", uploadID,
				"validation_errors", result.Errors,
				"record_error", recordErr,
			)
		}

		return HandleUploadResult{
			UploadID: uploadID,
			Valid:    false,
			Errors:   result.Errors,
		}
	}

	// --------------------------------------------------------------------------
	// Step 3 — Validation succeeded.  Emit metrics and proceed to publish.
	// --------------------------------------------------------------------------
	emitter.EmitValidationEvent(true, "", uploadType)
	slog.Info("handle upload: validation succeeded",
		"upload_id", uploadID,
		"upload_type", uploadType,
		"file_count", len(result.ValidatedFiles),
	)

	// --------------------------------------------------------------------------
	// Step 4 — Publish validated content to the immutable versioned prefix.
	//
	// Publisher.Publish writes the status "published" on success, or returns
	// an error.  On error, we record the failure and return — the active site
	// was NOT modified because Publish only writes S3 objects before the
	// atomic metadata update, and the atomic update is the last step.
	// --------------------------------------------------------------------------
	pubResult, err := pub.Publish(ctx, publish.PublishInput{
		UploadID:       uploadID,
		ValidatedFiles: result.ValidatedFiles,
		FileCount:      len(result.ValidatedFiles),
		TotalBytes:     totalBytes(result.ValidatedFiles),
	})
	if err != nil {
		emitter.EmitPublishEvent(false, err.Error())
		slog.Error("handle upload: publish failed",
			"upload_id", uploadID,
			"error", err,
		)

		// Record the publish failure on the upload record.
		// The active site was NOT modified.
		vr := &publish.ValidationResult{
			Valid:  false,
			Errors: []string{err.Error()},
		}
		if recordErr := pub.RecordValidationResult(ctx, upload.UserID, uploadID, vr); recordErr != nil {
			slog.Error("handle upload: failed to record publish failure",
				"upload_id", uploadID,
				"publish_error", err,
				"record_error", recordErr,
			)
		}

		return HandleUploadResult{
			UploadID: uploadID,
			Valid:    false,
			Errors:   []string{err.Error()},
		}
	}

	// --------------------------------------------------------------------------
	// Step 5 — Success.  The upload status is already "published" (set by
	// Publisher.Publish) and the active version pointer is advanced.
	// --------------------------------------------------------------------------
	emitter.EmitPublishEvent(true, "")
	slog.Info("handle upload: publish succeeded",
		"upload_id", uploadID,
		"version_id", pubResult.VersionID,
		"s3_prefix", pubResult.S3PublishedPrefix,
		"files_written", pubResult.FilesWritten,
	)

	return HandleUploadResult{
		UploadID:       uploadID,
		Valid:          true,
		VersionID:      pubResult.VersionID,
		FilesPublished: pubResult.FilesWritten,
	}
}

// totalBytes returns the sum of all byte slice lengths in the map.
func totalBytes(files map[string][]byte) int64 {
	var total int64
	for _, b := range files {
		total += int64(len(b))
	}
	return total
}
