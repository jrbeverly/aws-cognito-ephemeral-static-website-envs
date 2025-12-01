// ==============================================================================
// main.go — Backend API entrypoint
//
// Detects the execution environment and starts the appropriate runtime:
//   - Lambda mode:  implements the Lambda Runtime Interface for ALB target
//                   events; also serves the built portal from ./portal
//   - Local mode:   starts an HTTP server on the configured port
//
// Both modes use the same http.Handler built by api.NewRouter(), ensuring
// consistent behaviour between local development and deployed environments.
//
// No external Lambda framework is required — the Lambda Runtime Interface is
// a simple HTTP-based protocol (POST /runtime/invocation/next for events,
// POST /runtime/invocation/{id}/response for results).
// ==============================================================================

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"backend/go-api/internal/api"
	"backend/go-api/internal/config"
	"backend/go-api/internal/handler"
	metadb "backend/go-api/internal/metadata/dynamodb"
	"backend/go-api/internal/metrics"
)

func main() {
	cfg := config.Load()
	setupLogging(cfg)

	// Create DynamoDB client and wire up the metadata repositories.
	// When DYNAMODB_ENDPOINT is set (e.g., for LocalStack), the client
	// connects to that endpoint instead of the default AWS endpoint.
	// LoadDefaultConfig supplies credentials (the Lambda role in AWS); bare
	// Options{} sends unsigned requests, which only LocalStack accepts.
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(cfg.Region))
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}
	dbClient := dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		if cfg.DynamoDBEndpoint != "" {
			o.BaseEndpoint = aws.String(cfg.DynamoDBEndpoint)
		}
	})
	siteMetaRepo := metadb.NewSiteMetadataRepository(dbClient, cfg.SiteMetadataTable)
	uploadRecordsRepo := metadb.NewUploadRecordsRepository(dbClient, cfg.UploadRecordsTable)

	// Create S3 client for presigned URL generation and content storage.
	// When S3_ENDPOINT is set (e.g., for LocalStack), the client connects
	// to that endpoint instead of the default AWS endpoint.  For presigned
	// URLs to work with LocalStack, path-style addressing must be used.
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.S3Endpoint != ""
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
	})
	presignAdapter := &s3PresignAdapter{
		client: s3.NewPresignClient(s3Client),
	}
	contentStoreAdapter := &s3ContentStoreAdapter{
		client: s3Client,
	}

	emitter := metrics.NewEmitter()

	callbacks := &cognitoCallbackAdapter{
		client:     cognitoidentityprovider.NewFromConfig(awsCfg),
		userPoolID: cfg.CognitoUserPoolID,
		clientID:   os.Getenv("COGNITO_CLIENT_ID"),
	}
	sitesHandler := handler.NewSitesHandler(siteMetaRepo, callbacks, cfg.SitesDomain)
	uploadsHandler := handler.NewUploadsHandler(siteMetaRepo, uploadRecordsRepo, presignAdapter, contentStoreAdapter, callbacks, cfg.SitesBucket, cfg.SitesDomain, emitter)

	router := api.NewRouter(sitesHandler, uploadsHandler, emitter)

	if runningInLambda() {
		slog.Info("starting in Lambda mode")
		runLambda(withPortal(router, filepath.Join(os.Getenv("LAMBDA_TASK_ROOT"), "portal")))
	} else {
		addr := ":" + cfg.Port
		slog.Info("starting local server", "addr", addr)
		if err := http.ListenAndServe(addr, router); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}
}

// ==============================================================================
// Environment detection
// ==============================================================================

func runningInLambda() bool {
	return os.Getenv("AWS_LAMBDA_RUNTIME_API") != ""
}

// ==============================================================================
// Logging setup
// ==============================================================================

func setupLogging(cfg *config.Config) {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}

// ==============================================================================
// Lambda Runtime Interface
//
// The AWS Lambda Runtime Interface is a simple HTTP API exposed by the Lambda
// execution environment at http://${AWS_LAMBDA_RUNTIME_API}/2018-06-01/runtime/.
//
// Loop:
//   GET  /runtime/invocation/next         → receive next event
//   POST /runtime/invocation/{id}/response → respond to the event
//   POST /runtime/invocation/{id}/error    → report an error
//
// For Go 1.22+ with provided.al2023 runtime, the bootstrap binary implements
// this protocol directly.  No external Lambda framework is required.
// ==============================================================================

func runLambda(handler http.Handler) {
	runtimeAPI := os.Getenv("AWS_LAMBDA_RUNTIME_API")
	client := &http.Client{}

	for {
		invocationURL := fmt.Sprintf("http://%s/2018-06-01/runtime/invocation/next", runtimeAPI)

		resp, err := client.Get(invocationURL)
		if err != nil {
			slog.Error("failed to get next invocation", "error", err)
			continue
		}

		// Read the request ID from headers
		requestID := resp.Header.Get("Lambda-Runtime-Aws-Request-Id")

		// Read the event body
		eventBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			slog.Error("failed to read event body", "error", err)
			postInvocationError(client, runtimeAPI, requestID, err)
			continue
		}

		// Process the event and post the response
		responseBody := processEvent(handler, eventBody)

		responseURL := fmt.Sprintf("http://%s/2018-06-01/runtime/invocation/%s/response", runtimeAPI, requestID)
		postErr := postResponse(client, responseURL, responseBody)
		if postErr != nil {
			slog.Error("failed to post response", "error", postErr)
			postInvocationError(client, runtimeAPI, requestID, postErr)
		}
	}
}

// processEvent converts an ALB Lambda target event to an http.Handler call and
// returns the serialized ALB response.  Bodies are always returned base64
// encoded so portal assets (fonts, images) survive intact.
func processEvent(handler http.Handler, eventBody []byte) []byte {
	var event albEvent
	if err := json.Unmarshal(eventBody, &event); err != nil {
		slog.Error("failed to parse event", "error", err)
		return internalErrorBody("failed to parse event")
	}

	body := []byte(event.Body)
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(event.Body)
		if err != nil {
			return internalErrorBody("failed to decode body")
		}
		body = decoded
	}

	// ALB passes query values still URL-encoded.
	q := url.Values{}
	for k, v := range event.QueryStringParameters {
		dk, _ := url.QueryUnescape(k)
		dv, _ := url.QueryUnescape(v)
		q.Set(dk, dv)
	}

	httpReq, err := http.NewRequest(event.HTTPMethod, event.Path, bytes.NewReader(body))
	if err != nil {
		return internalErrorBody("failed to create request")
	}
	httpReq.URL.RawQuery = q.Encode()
	httpReq = httpReq.WithContext(context.Background())
	for k, v := range event.Headers {
		httpReq.Header.Set(k, v)
	}

	rec := &responseRecorder{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
	handler.ServeHTTP(rec, httpReq)

	respHeaders := make(map[string]string)
	for k := range rec.header {
		respHeaders[k] = rec.header.Get(k)
	}

	respBytes, err := json.Marshal(albResponse{
		StatusCode:        rec.statusCode,
		StatusDescription: fmt.Sprintf("%d %s", rec.statusCode, http.StatusText(rec.statusCode)),
		Headers:           respHeaders,
		Body:              base64.StdEncoding.EncodeToString([]byte(rec.body.String())),
		IsBase64Encoded:   true,
	})
	if err != nil {
		return internalErrorBody("failed to serialize response")
	}
	return respBytes
}

// ==============================================================================
// ALB Lambda target event/response types
// https://docs.aws.amazon.com/elasticloadbalancing/latest/application/lambda-functions.html
// ==============================================================================

type albEvent struct {
	HTTPMethod            string            `json:"httpMethod"`
	Path                  string            `json:"path"`
	QueryStringParameters map[string]string `json:"queryStringParameters"`
	Headers               map[string]string `json:"headers"`
	Body                  string            `json:"body"`
	IsBase64Encoded       bool              `json:"isBase64Encoded"`
}

type albResponse struct {
	StatusCode        int               `json:"statusCode"`
	StatusDescription string            `json:"statusDescription"`
	Headers           map[string]string `json:"headers,omitempty"`
	Body              string            `json:"body"`
	IsBase64Encoded   bool              `json:"isBase64Encoded"`
}

// ==============================================================================
// Portal — the built Vue app is packaged next to the bootstrap binary
// ==============================================================================

// withPortal routes /api/* and /health to the API and serves every other path
// from dir, falling back to index.html so client-side routes load.
func withPortal(apiHandler http.Handler, dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		file := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(file); err != nil || info.IsDir() {
			file = filepath.Join(dir, "index.html")
		}
		http.ServeFile(w, r, file)
	})
}

// ==============================================================================
// HTTP helpers for Lambda Runtime Interface
// ==============================================================================

func postResponse(client *http.Client, url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func postInvocationError(client *http.Client, runtimeAPI, requestID string, err error) {
	errorURL := fmt.Sprintf("http://%s/2018-06-01/runtime/invocation/%s/error", runtimeAPI, requestID)
	errorBody := fmt.Sprintf(`{"errorMessage":"%s"}`, err.Error())
	req, _ := http.NewRequest(http.MethodPost, errorURL, strings.NewReader(errorBody))
	req.Header.Set("Content-Type", "application/json")
	resp, reqErr := client.Do(req)
	if reqErr != nil {
		slog.Error("failed to post invocation error", "error", reqErr)
		return
	}
	resp.Body.Close()
}

func internalErrorBody(message string) []byte {
	return []byte(fmt.Sprintf(
		`{"statusCode":500,"statusDescription":"500 Internal Server Error","headers":{"Content-Type":"application/json"},"body":"{\"error\":{\"code\":\"INTERNAL_ERROR\",\"message\":\"%s\"}}","isBase64Encoded":false}`,
		message,
	))
}

// ==============================================================================
// Response recorder — http.ResponseWriter implementation for Lambda adapter
// ==============================================================================

type responseRecorder struct {
	header     http.Header
	body       strings.Builder
	statusCode int
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	return r.body.Write(data)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

// ==============================================================================
// S3 presign adapter — bridges handler.PresignClient to the AWS SDK
//
// The handler package defines a PresignClient interface that avoids coupling
// application code to the AWS SDK.  This adapter implements that interface
// using the real s3.PresignClient, translating between the handler-domain
// PresignRequest / PresignResult types and the SDK's PutObjectInput /
// PresignedPutObjectOutput.
// ==============================================================================

// s3PresignAdapter implements handler.PresignClient using the AWS SDK v2
// s3.PresignClient.  It is wired in main() and passed to the UploadsHandler.
type s3PresignAdapter struct {
	client *s3.PresignClient
}

// PresignPutObject generates a presigned PUT URL via the AWS SDK.
func (a *s3PresignAdapter) PresignPutObject(ctx context.Context, req handler.PresignRequest) (*handler.PresignResult, error) {
	result, err := a.client.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(req.Bucket),
		Key:         aws.String(req.Key),
		ContentType: aws.String(req.ContentType),
	}, s3.WithPresignExpires(req.Expires))
	if err != nil {
		return nil, err
	}
	return &handler.PresignResult{
		URL:       result.URL,
		ExpiresAt: time.Now().Add(req.Expires),
	}, nil
}

// ==============================================================================
// S3 content store adapter — bridges handler.ContentStore to the AWS SDK
//
// The handler package defines a ContentStore interface that avoids coupling
// application code to the AWS SDK.  This adapter implements that interface
// using the real s3.Client.PutObject, translating between the handler-domain
// ContentStoreRequest type and the SDK's PutObjectInput.
// ==============================================================================

// s3ContentStoreAdapter implements handler.ContentStore using the AWS SDK v2
// s3.Client.  It is wired in main() and passed to the UploadsHandler.
type s3ContentStoreAdapter struct {
	client *s3.Client
}

// PutContent writes data directly to an S3 object via the AWS SDK.
func (a *s3ContentStoreAdapter) PutContent(ctx context.Context, req handler.ContentStoreRequest) error {
	_, err := a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(req.Bucket),
		Key:         aws.String(req.Key),
		Body:        bytes.NewReader(req.Body),
		ContentType: aws.String(req.ContentType),
	})
	return err
}

// GetContent reads an S3 object (used to publish staged uploads).
func (a *s3ContentStoreAdapter) GetContent(ctx context.Context, bucket, key string) ([]byte, error) {
	out, err := a.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// ==============================================================================
// Cognito callback adapter — implements handler.CallbackRegistrar
//
// UpdateUserPoolClient resets every setting it is not given, so the current
// client is read and written back whole with the URL appended.  Without a
// client ID (local development) registration is skipped.
// ==============================================================================

type cognitoCallbackAdapter struct {
	client     *cognitoidentityprovider.Client
	userPoolID string
	clientID   string
}

func (a *cognitoCallbackAdapter) AddCallbackURL(ctx context.Context, url string) error {
	if a.clientID == "" {
		return nil
	}
	out, err := a.client.DescribeUserPoolClient(ctx, &cognitoidentityprovider.DescribeUserPoolClientInput{
		UserPoolId: aws.String(a.userPoolID),
		ClientId:   aws.String(a.clientID),
	})
	if err != nil {
		return err
	}
	c := out.UserPoolClient
	for _, existing := range c.CallbackURLs {
		if existing == url {
			return nil
		}
	}
	_, err = a.client.UpdateUserPoolClient(ctx, &cognitoidentityprovider.UpdateUserPoolClientInput{
		UserPoolId:                               c.UserPoolId,
		ClientId:                                 c.ClientId,
		ClientName:                               c.ClientName,
		RefreshTokenValidity:                     c.RefreshTokenValidity,
		AccessTokenValidity:                      c.AccessTokenValidity,
		IdTokenValidity:                          c.IdTokenValidity,
		TokenValidityUnits:                       c.TokenValidityUnits,
		ReadAttributes:                           c.ReadAttributes,
		WriteAttributes:                          c.WriteAttributes,
		ExplicitAuthFlows:                        c.ExplicitAuthFlows,
		SupportedIdentityProviders:               c.SupportedIdentityProviders,
		CallbackURLs:                             append(c.CallbackURLs, url),
		LogoutURLs:                               c.LogoutURLs,
		DefaultRedirectURI:                       c.DefaultRedirectURI,
		AllowedOAuthFlows:                        c.AllowedOAuthFlows,
		AllowedOAuthScopes:                       c.AllowedOAuthScopes,
		AllowedOAuthFlowsUserPoolClient:          aws.ToBool(c.AllowedOAuthFlowsUserPoolClient),
		AnalyticsConfiguration:                   c.AnalyticsConfiguration,
		PreventUserExistenceErrors:               c.PreventUserExistenceErrors,
		EnableTokenRevocation:                    c.EnableTokenRevocation,
		EnablePropagateAdditionalUserContextData: c.EnablePropagateAdditionalUserContextData,
		AuthSessionValidity:                      c.AuthSessionValidity,
	})
	return err
}
