// ==============================================================================
// site_metadata.go — DynamoDB implementation of SiteMetadataRepository
//
// Implements the single-table design from infrastructure/aws/modules/dynamodb/main.tf:
//
//   Entity       PK                  SK                  Key attributes
//   -----------  ------------------  ------------------  --------------------------
//   User         USER#<userId>       PROFILE             userSlug, cognitoSub, ...
//   Site         USER#<userId>       SITE#<siteId>       siteSlug, activeVersionId, ...
//   HostMapping  HOST#<hostname>     MAPPING             userId, userSlug, siteId, ...
//
// Access patterns:
//   - List a user's sites:     Query  pk=USER#<userId>, sk begins_with SITE#
//   - Host → serving chain:    GetItem pk=HOST#<hostname>, sk=MAPPING
//   - Get user profile:        GetItem pk=USER#<userId>, sk=PROFILE
//
// Atomic active-version updates use TransactWriteItems to keep the site
// and host mapping in sync.
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
// Primary-key and sort-key prefixes — must match the table design
// ==============================================================================

const (
	pkUser  = "USER#"
	skUser  = "PROFILE"
	pkHost  = "HOST#"
	skHost  = "MAPPING"
	skSite  = "SITE#"
)

// ==============================================================================
// siteMetadataRepo — private implementation of SiteMetadataRepository
// ==============================================================================

type siteMetadataRepo struct {
	client    DynamoDBAPI
	tableName string
}

// NewSiteMetadataRepository creates a DynamoDB-backed SiteMetadataRepository.
// client satisfies DynamoDBAPI — pass a *dynamodb.Client in production or
// a mock in tests.
// tableName is the physical name of the site_metadata DynamoDB table
// (e.g. "sites-platform-lab-metadata").
func NewSiteMetadataRepository(client DynamoDBAPI, tableName string) metadata.SiteMetadataRepository {
	return &siteMetadataRepo{client: client, tableName: tableName}
}

// ==============================================================================
// User operations
// ==============================================================================

// userItem is the DynamoDB shape for a User entity.
type userItem struct {
	PK         string `dynamodbav:"pk"`
	SK         string `dynamodbav:"sk"`
	UserSlug   string `dynamodbav:"userSlug"`
	CognitoSub string `dynamodbav:"cognitoSub"`
	CreatedAt  string `dynamodbav:"createdAt"` // ISO 8601
	UpdatedAt  string `dynamodbav:"updatedAt"`
	Disabled   bool   `dynamodbav:"disabled"`
}

func userPK(userID string) string { return pkUser + userID }

func (r *siteMetadataRepo) CreateUser(ctx context.Context, user metadata.User) error {
	now := time.Now().UTC().Format(time.RFC3339)
	item := userItem{
		PK:         userPK(user.UserID),
		SK:         skUser,
		UserSlug:   user.UserSlug,
		CognitoSub: user.CognitoSub,
		CreatedAt:  now,
		UpdatedAt:  now,
		Disabled:   false,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal user item: %w", err)
	}

	_, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName),
		Item:      av,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("user %s already exists", user.UserID)
		}
		return fmt.Errorf("put user item: %w", err)
	}
	return nil
}

func (r *siteMetadataRepo) GetUser(ctx context.Context, userID string) (*metadata.User, error) {
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(userID)},
			"sk": &types.AttributeValueMemberS{Value: skUser},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get user item: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	var item userItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return nil, fmt.Errorf("unmarshal user item: %w", err)
	}
	return &metadata.User{
		UserID:     userID,
		UserSlug:   item.UserSlug,
		CognitoSub: item.CognitoSub,
		CreatedAt:  mustParseTime(item.CreatedAt),
		UpdatedAt:  mustParseTime(item.UpdatedAt),
		Disabled:   item.Disabled,
	}, nil
}

func (r *siteMetadataRepo) UpdateUser(ctx context.Context, user metadata.User) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(user.UserID)},
			"sk": &types.AttributeValueMemberS{Value: skUser},
		},
		ConditionExpression: aws.String("attribute_exists(pk)"),
		UpdateExpression:    aws.String("SET userSlug = :slug, #disabled = :dis, updatedAt = :now"),
		ExpressionAttributeNames: map[string]string{
			"#disabled": "disabled",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":slug": &types.AttributeValueMemberS{Value: user.UserSlug},
			":dis":  &types.AttributeValueMemberBOOL{Value: user.Disabled},
			":now":  &types.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("user %s not found", user.UserID)
		}
		return fmt.Errorf("update user item: %w", err)
	}
	return nil
}

func (r *siteMetadataRepo) DeleteUser(ctx context.Context, userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(userID)},
			"sk": &types.AttributeValueMemberS{Value: skUser},
		},
		ConditionExpression: aws.String("attribute_exists(pk)"),
		UpdateExpression:    aws.String("SET #disabled = :t, updatedAt = :now"),
		ExpressionAttributeNames: map[string]string{
			"#disabled": "disabled",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":t":   &types.AttributeValueMemberBOOL{Value: true},
			":now": &types.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("user %s not found", userID)
		}
		return fmt.Errorf("delete (disable) user: %w", err)
	}
	return nil
}

// ==============================================================================
// Site operations
// ==============================================================================

// siteItem is the DynamoDB shape for a Site entity.
type siteItem struct {
	PK                string `dynamodbav:"pk"`
	SK                string `dynamodbav:"sk"`
	SiteSlug          string `dynamodbav:"siteSlug"`
	ActiveVersionID   string `dynamodbav:"activeVersionId"`
	S3PublishedPrefix string `dynamodbav:"s3PublishedPrefix"`
	CreatedAt         string `dynamodbav:"createdAt"`
	UpdatedAt         string `dynamodbav:"updatedAt"`
	Disabled          bool   `dynamodbav:"disabled"`
}

func siteSK(siteID string) string { return skSite + siteID }

func (r *siteMetadataRepo) CreateSite(ctx context.Context, site metadata.Site) error {
	now := time.Now().UTC().Format(time.RFC3339)
	item := siteItem{
		PK:                userPK(site.UserID),
		SK:                siteSK(site.SiteID),
		SiteSlug:          site.SiteSlug,
		ActiveVersionID:   site.ActiveVersionID,
		S3PublishedPrefix: site.S3PublishedPrefix,
		CreatedAt:         now,
		UpdatedAt:         now,
		Disabled:          false,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal site item: %w", err)
	}

	_, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(r.tableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("site %s already exists for user %s", site.SiteID, site.UserID)
		}
		return fmt.Errorf("put site item: %w", err)
	}
	return nil
}

func (r *siteMetadataRepo) GetSite(ctx context.Context, userID, siteID string) (*metadata.Site, error) {
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(userID)},
			"sk": &types.AttributeValueMemberS{Value: siteSK(siteID)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get site item: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	var item siteItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return nil, fmt.Errorf("unmarshal site item: %w", err)
	}
	return &metadata.Site{
		SiteID:            siteID,
		UserID:            userID,
		SiteSlug:          item.SiteSlug,
		ActiveVersionID:   item.ActiveVersionID,
		S3PublishedPrefix: item.S3PublishedPrefix,
		CreatedAt:         mustParseTime(item.CreatedAt),
		UpdatedAt:         mustParseTime(item.UpdatedAt),
		Disabled:          item.Disabled,
	}, nil
}

func (r *siteMetadataRepo) ListSites(ctx context.Context, userID string) ([]metadata.Site, error) {
	out, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(r.tableName),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: userPK(userID)},
			":prefix": &types.AttributeValueMemberS{Value: skSite},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query sites: %w", err)
	}

	sites := make([]metadata.Site, 0, len(out.Items))
	for _, av := range out.Items {
		var item siteItem
		if err := attributevalue.UnmarshalMap(av, &item); err != nil {
			return nil, fmt.Errorf("unmarshal site item in list: %w", err)
		}
		// Derive siteID from SK: "SITE#<siteId>"
		siteID := item.SK[len(skSite):]
		sites = append(sites, metadata.Site{
			SiteID:            siteID,
			UserID:            userID,
			SiteSlug:          item.SiteSlug,
			ActiveVersionID:   item.ActiveVersionID,
			S3PublishedPrefix: item.S3PublishedPrefix,
			CreatedAt:         mustParseTime(item.CreatedAt),
			UpdatedAt:         mustParseTime(item.UpdatedAt),
			Disabled:          item.Disabled,
		})
	}
	return sites, nil
}

func (r *siteMetadataRepo) UpdateSite(ctx context.Context, site metadata.Site) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(site.UserID)},
			"sk": &types.AttributeValueMemberS{Value: siteSK(site.SiteID)},
		},
		ConditionExpression: aws.String("attribute_exists(pk)"),
		UpdateExpression:    aws.String("SET siteSlug = :slug, #disabled = :dis, updatedAt = :now"),
		ExpressionAttributeNames: map[string]string{
			"#disabled": "disabled",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":slug": &types.AttributeValueMemberS{Value: site.SiteSlug},
			":dis":  &types.AttributeValueMemberBOOL{Value: site.Disabled},
			":now":  &types.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("site %s not found for user %s", site.SiteID, site.UserID)
		}
		return fmt.Errorf("update site item: %w", err)
	}
	return nil
}

func (r *siteMetadataRepo) DeleteSite(ctx context.Context, userID, siteID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: userPK(userID)},
			"sk": &types.AttributeValueMemberS{Value: siteSK(siteID)},
		},
		ConditionExpression: aws.String("attribute_exists(pk)"),
		UpdateExpression:    aws.String("SET #disabled = :t, updatedAt = :now"),
		ExpressionAttributeNames: map[string]string{
			"#disabled": "disabled",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":t":   &types.AttributeValueMemberBOOL{Value: true},
			":now": &types.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("site %s not found for user %s", siteID, userID)
		}
		return fmt.Errorf("delete (disable) site: %w", err)
	}
	return nil
}

// ==============================================================================
// Atomic active-version update
//
// Uses TransactWriteItems to atomically update the site's active version
// pointer and the corresponding host mapping in a single transaction.
// Both updates succeed or neither does.
// ==============================================================================

func (r *siteMetadataRepo) UpdateActiveVersion(ctx context.Context, userID, siteID, versionID, s3PublishedPrefix, sitesDomain string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	site, err := r.GetSite(ctx, userID, siteID)
	if err != nil {
		return fmt.Errorf("read site before version update: %w", err)
	}
	if site == nil {
		return fmt.Errorf("site %s not found for user %s", siteID, userID)
	}

	user, err := r.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("read user before version update: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user %s not found", userID)
	}

	// Construct both hostname patterns with the full domain suffix.
	// These must match the hostnames created in handler/sites.go CreateSite.
	//   Preferred: {siteSlug}.{userSlug}.{sitesDomain}
	//   Alias:     {siteSlug}--{userSlug}.{sitesDomain}
	preferredHost := fmt.Sprintf("%s.%s.%s", site.SiteSlug, user.UserSlug, sitesDomain)
	aliasHost := fmt.Sprintf("%s--%s.%s", site.SiteSlug, user.UserSlug, sitesDomain)

	// Build the transaction — update site + both host mappings together.
	transactItems := []types.TransactWriteItem{
		// Update the site: set activeVersionId and s3PublishedPrefix.
		{
			Update: &types.Update{
				TableName: aws.String(r.tableName),
				Key: map[string]types.AttributeValue{
					"pk": &types.AttributeValueMemberS{Value: userPK(userID)},
					"sk": &types.AttributeValueMemberS{Value: siteSK(siteID)},
				},
				ConditionExpression: aws.String("attribute_exists(pk) AND #disabled <> :t"),
				UpdateExpression:    aws.String("SET activeVersionId = :vid, s3PublishedPrefix = :prefix, updatedAt = :now"),
				ExpressionAttributeNames: map[string]string{
					"#disabled": "disabled",
				},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":vid":    &types.AttributeValueMemberS{Value: versionID},
					":prefix": &types.AttributeValueMemberS{Value: s3PublishedPrefix},
					":now":    &types.AttributeValueMemberS{Value: now},
					":t":      &types.AttributeValueMemberBOOL{Value: true},
				},
			},
		},
	}

	// Conditionally update each host mapping that exists.  We use
	// attribute_exists for idempotency — a missing mapping doesn't block
	// the transaction on the first publish (some environments may only
	// create one pattern).  On subsequent publishes, both will be updated.
	for _, hostname := range []string{preferredHost, aliasHost} {
		transactItems = append(transactItems, types.TransactWriteItem{
			Update: &types.Update{
				TableName: aws.String(r.tableName),
				Key: map[string]types.AttributeValue{
					"pk": &types.AttributeValueMemberS{Value: pkHost + hostname},
					"sk": &types.AttributeValueMemberS{Value: skHost},
				},
				ConditionExpression: aws.String("attribute_exists(pk)"),
				UpdateExpression:    aws.String("SET versionId = :vid, s3PublishedPrefix = :prefix"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":vid":    &types.AttributeValueMemberS{Value: versionID},
					":prefix": &types.AttributeValueMemberS{Value: s3PublishedPrefix},
				},
			},
		})
	}

	_, err = r.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: transactItems,
	})
	if err != nil {
		var tcfe *types.TransactionCanceledException
		if errors.As(err, &tcfe) {
			return fmt.Errorf("atomic version update cancelled — site or host mapping may not exist or be disabled")
		}
		return fmt.Errorf("transact write active version: %w", err)
	}
	return nil
}

// ==============================================================================
// Host mapping operations
// ==============================================================================

// hostMappingItem is the DynamoDB shape for a HostMapping entity.
type hostMappingItem struct {
	PK                string `dynamodbav:"pk"`
	SK                string `dynamodbav:"sk"`
	UserID            string `dynamodbav:"userId"`
	UserSlug          string `dynamodbav:"userSlug"`
	SiteID            string `dynamodbav:"siteId"`
	SiteSlug          string `dynamodbav:"siteSlug"`
	VersionID         string `dynamodbav:"versionId"`
	S3PublishedPrefix string `dynamodbav:"s3PublishedPrefix"`
	UserDisabled      bool   `dynamodbav:"userDisabled"`
	SiteDisabled      bool   `dynamodbav:"siteDisabled"`
}

func (r *siteMetadataRepo) CreateHostMapping(ctx context.Context, mapping metadata.HostMapping) error {
	item := hostMappingItem{
		PK:                pkHost + mapping.Hostname,
		SK:                skHost,
		UserID:            mapping.UserID,
		UserSlug:          mapping.UserSlug,
		SiteID:            mapping.SiteID,
		SiteSlug:          mapping.SiteSlug,
		VersionID:         mapping.VersionID,
		S3PublishedPrefix: mapping.S3PublishedPrefix,
		UserDisabled:      false,
		SiteDisabled:      false,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal host mapping item: %w", err)
	}

	_, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(r.tableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("host mapping for %s already exists", mapping.Hostname)
		}
		return fmt.Errorf("put host mapping: %w", err)
	}
	return nil
}

func (r *siteMetadataRepo) GetHostMapping(ctx context.Context, hostname string) (*metadata.HostMapping, error) {
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pkHost + hostname},
			"sk": &types.AttributeValueMemberS{Value: skHost},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get host mapping: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	var item hostMappingItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return nil, fmt.Errorf("unmarshal host mapping: %w", err)
	}
	return &metadata.HostMapping{
		Hostname:          hostname,
		UserID:            item.UserID,
		UserSlug:          item.UserSlug,
		SiteID:            item.SiteID,
		SiteSlug:          item.SiteSlug,
		VersionID:         item.VersionID,
		S3PublishedPrefix: item.S3PublishedPrefix,
		UserDisabled:      item.UserDisabled,
		SiteDisabled:      item.SiteDisabled,
	}, nil
}

func (r *siteMetadataRepo) UpdateHostMapping(ctx context.Context, hostname string, mapping metadata.HostMapping) error {
	_, err := r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pkHost + hostname},
			"sk": &types.AttributeValueMemberS{Value: skHost},
		},
		ConditionExpression: aws.String("attribute_exists(pk)"),
		UpdateExpression:    aws.String("SET userDisabled = :ud, siteDisabled = :sd, versionId = :vid, s3PublishedPrefix = :prefix"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":ud":     &types.AttributeValueMemberBOOL{Value: mapping.UserDisabled},
			":sd":     &types.AttributeValueMemberBOOL{Value: mapping.SiteDisabled},
			":vid":    &types.AttributeValueMemberS{Value: mapping.VersionID},
			":prefix": &types.AttributeValueMemberS{Value: mapping.S3PublishedPrefix},
		},
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("host mapping for %s not found", hostname)
		}
		return fmt.Errorf("update host mapping: %w", err)
	}
	return nil
}

func (r *siteMetadataRepo) DeleteHostMapping(ctx context.Context, hostname string) error {
	_, err := r.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pkHost + hostname},
			"sk": &types.AttributeValueMemberS{Value: skHost},
		},
		ConditionExpression: aws.String("attribute_exists(pk)"),
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("host mapping for %s not found", hostname)
		}
		return fmt.Errorf("delete host mapping: %w", err)
	}
	return nil
}

// ==============================================================================
// Helpers
// ==============================================================================

// mustParseTime parses an ISO 8601 timestamp string to time.Time.
// Returns time.Time{} on failure — the stored timestamps are always valid,
// so a parse failure indicates programmer error caught by tests.
func mustParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
