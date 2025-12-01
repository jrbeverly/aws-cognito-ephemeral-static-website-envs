// ==============================================================================
// upload_records.go — DynamoDB implementation of UploadRecordsRepository
//
// Implements the table design from infrastructure/aws/modules/dynamodb/main.tf:
//
//   Entity   PK                SK                  upload_id (GSI-1 PK)
//   -------  ----------------  ------------------  --------------------
//   Upload   USER#<userId>     UPLOAD#<uploadId>   <uploadId>
//
// Access patterns:
//   - List uploads for a user:         Query pk=USER#<userId>,
//                                      sk begins_with UPLOAD#
//   - Look up upload by ID (status):   Query GSI upload-by-id,
//                                      pk=<uploadId>
//   - Get specific upload:             GetItem pk=USER#<userId>,
//                                      sk=UPLOAD#<uploadId>
// ==============================================================================

package dynamodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"backend/go-api/internal/metadata"
)

// ==============================================================================
// Upload primary-key and sort-key prefixes
// ==============================================================================

const (
	skUpload          = "UPLOAD#"
	gsiUploadByID     = "upload-by-id" // Must match the GSI name in the Terraform module
	gsiUploadByIDKey  = "upload_id"    // GSI hash key attribute name
)

// ==============================================================================
// uploadRecordsRepo — private implementation of UploadRecordsRepository
// ==============================================================================

type uploadRecordsRepo struct {
	client    DynamoDBAPI
	tableName string
}

// NewUploadRecordsRepository creates a DynamoDB-backed UploadRecordsRepository.
// client satisfies DynamoDBAPI — pass a *dynamodb.Client in production or
// a mock in tests.
// tableName is the physical name of the upload_records DynamoDB table
// (e.g. "sites-platform-lab-uploads").
func NewUploadRecordsRepository(client DynamoDBAPI, tableName string) metadata.UploadRecordsRepository {
	return &uploadRecordsRepo{client: client, tableName: tableName}
}

// ==============================================================================
// uploadItem — DynamoDB shape for an Upload entity
// ==============================================================================

type uploadItem struct {
	PK                   string                `dynamodbav:"pk"`
	SK                   string                `dynamodbav:"sk"`
	UploadID             string                `dynamodbav:"upload_id"` // GSI hash key
	SiteID               string                `dynamodbav:"siteId"`
	Status               string                `dynamodbav:"status"`
	ValidationResultJSON string                `dynamodbav:"validationResult,omitempty"` // JSON-serialised; empty when nil
	S3StagingKey         string                `dynamodbav:"s3StagingKey"`
	CompressedSizeBytes  int64                 `dynamodbav:"compressedSizeBytes"`
	UncompressedSizeBytes int64                `dynamodbav:"uncompressedSizeBytes"`
	FileCount            int                   `dynamodbav:"fileCount"`
	CreatedAt            string                `dynamodbav:"createdAt"` // ISO 8601
	UpdatedAt            string                `dynamodbav:"updatedAt"`
}

func uploadSK(uploadID string) string { return skUpload + uploadID }

// ==============================================================================
// Create / Read / List
// ==============================================================================

func (r *uploadRecordsRepo) CreateUpload(ctx context.Context, upload metadata.Upload) error {
	now := time.Now().UTC().Format(time.RFC3339)

	item := uploadItem{
		PK:                   userPK(upload.UserID),
		SK:                   uploadSK(upload.UploadID),
		UploadID:             upload.UploadID,
		SiteID:               upload.SiteID,
		Status:               string(upload.Status),
		S3StagingKey:         upload.S3StagingKey,
		CompressedSizeBytes:  upload.CompressedSizeBytes,
		UncompressedSizeBytes: upload.UncompressedSizeBytes,
		FileCount:            upload.FileCount,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	// Only serialise validation result when it is non-nil.
	if upload.ValidationResult != nil {
		vrJSON, err := marshalValidationResult(upload.ValidationResult)
		if err != nil {
			return fmt.Errorf("marshal validation result: %w", err)
		}
		item.ValidationResultJSON = vrJSON
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal upload item: %w", err)
	}

	_, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(r.tableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("upload %s already exists for user %s", upload.UploadID, upload.UserID)
		}
		return fmt.Errorf("put upload item: %w", err)
	}
	return nil
}

func (r *uploadRecordsRepo) GetUpload(ctx context.Context, userID, uploadID string) (*metadata.Upload, error) {
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(userID)},
			"sk": &types.AttributeValueMemberS{Value: uploadSK(uploadID)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get upload item: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	return unmarshalUpload(out.Item)
}

func (r *uploadRecordsRepo) GetUploadByID(ctx context.Context, uploadID string) (*metadata.Upload, error) {
	out, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(r.tableName),
		IndexName:              aws.String(gsiUploadByID),
		KeyConditionExpression: aws.String("upload_id = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: uploadID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query upload by id GSI: %w", err)
	}
	if len(out.Items) == 0 {
		return nil, nil
	}
	return unmarshalUpload(out.Items[0])
}

func (r *uploadRecordsRepo) ListUploads(ctx context.Context, userID string) ([]metadata.Upload, error) {
	out, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(r.tableName),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: userPK(userID)},
			":prefix": &types.AttributeValueMemberS{Value: skUpload},
		},
		// Return most recent first (DynamoDB sort-key order; with SK=UPLOAD#<id>
		// the default ascending order is by upload ID, not creation time.
		// For time-based ordering we would need a GSI on createdAt.  For the
		// prototype, we return in SK order and let callers sort if needed.
		ScanIndexForward: aws.Bool(false), // descending SK order for recent-first
	})
	if err != nil {
		return nil, fmt.Errorf("query uploads: %w", err)
	}

	uploads := make([]metadata.Upload, 0, len(out.Items))
	for _, av := range out.Items {
		u, err := unmarshalUpload(av)
		if err != nil {
			return nil, fmt.Errorf("unmarshal upload in list: %w", err)
		}
		uploads = append(uploads, *u)
	}
	return uploads, nil
}

// ==============================================================================
// Status updates
// ==============================================================================

func (r *uploadRecordsRepo) UpdateUploadStatus(ctx context.Context, userID, uploadID string, status metadata.UploadStatus, result *metadata.ValidationResult) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var vrJSON string
	if result != nil {
		var err error
		vrJSON, err = marshalValidationResult(result)
		if err != nil {
			return fmt.Errorf("marshal validation result: %w", err)
		}
	}

	updateExpr := "SET #status = :st, updatedAt = :now"
	exprNames := map[string]string{
		"#status": "status",
	}
	exprValues := map[string]types.AttributeValue{
		":st":   &types.AttributeValueMemberS{Value: string(status)},
		":now":  &types.AttributeValueMemberS{Value: now},
	}

	if result != nil {
		updateExpr += ", validationResult = :vr"
		exprValues[":vr"] = &types.AttributeValueMemberS{Value: vrJSON}
	}

	_, err := r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(userID)},
			"sk": &types.AttributeValueMemberS{Value: uploadSK(uploadID)},
		},
		ConditionExpression:         aws.String("attribute_exists(pk)"),
		UpdateExpression:            aws.String(updateExpr),
		ExpressionAttributeNames:    exprNames,
		ExpressionAttributeValues:   exprValues,
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("upload %s not found for user %s", uploadID, userID)
		}
		return fmt.Errorf("update upload status: %w", err)
	}
	return nil
}

// ==============================================================================
// Helpers
// ==============================================================================

// unmarshalUpload converts a DynamoDB item map to a metadata.Upload.
func unmarshalUpload(av map[string]types.AttributeValue) (*metadata.Upload, error) {
	var item uploadItem
	if err := attributevalue.UnmarshalMap(av, &item); err != nil {
		return nil, err
	}

	// Derive userID from PK: "USER#<userId>"
	userID := ""
	if len(item.PK) > len(pkUser) {
		userID = item.PK[len(pkUser):]
	}

	upload := &metadata.Upload{
		UploadID:             item.UploadID,
		UserID:               userID,
		SiteID:               item.SiteID,
		Status:               metadata.UploadStatus(item.Status),
		S3StagingKey:         item.S3StagingKey,
		CompressedSizeBytes:  item.CompressedSizeBytes,
		UncompressedSizeBytes: item.UncompressedSizeBytes,
		FileCount:            item.FileCount,
		CreatedAt:            mustParseTime(item.CreatedAt),
		UpdatedAt:            mustParseTime(item.UpdatedAt),
	}

	if item.ValidationResultJSON != "" {
		vr, err := unmarshalValidationResult(item.ValidationResultJSON)
		if err != nil {
			return nil, fmt.Errorf("unmarshal validation result: %w", err)
		}
		upload.ValidationResult = vr
	}

	return upload, nil
}

// marshalValidationResult serialises a ValidationResult to a JSON string
// for inline storage on the upload item.
func marshalValidationResult(vr *metadata.ValidationResult) (string, error) {
	if vr == nil {
		return "", nil
	}
	return marshalJSON(vr), nil
}

func unmarshalValidationResult(s string) (*metadata.ValidationResult, error) {
	return unmarshalJSON[metadata.ValidationResult](s), nil
}
