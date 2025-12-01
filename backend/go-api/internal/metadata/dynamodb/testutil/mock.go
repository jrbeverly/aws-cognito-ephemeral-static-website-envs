// ==============================================================================
// mock.go — In-memory DynamoDB mock for unit tests
//
// Implements the metadb.DynamoDBAPI interface using in-memory maps.
// Supports PutItem, GetItem, UpdateItem, DeleteItem, Query, and
// TransactWriteItems with enough DynamoDB semantics (conditional checks,
// expression evaluation) to validate repository behaviour.
// ==============================================================================

package testutil

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	metadb "backend/go-api/internal/metadata/dynamodb"
)

// MockDynamoDB is an in-memory DynamoDB client for testing.
// It satisfies the metadb.DynamoDBAPI interface.
type MockDynamoDB struct {
	mu    sync.RWMutex
	items map[string]map[string]map[string]types.AttributeValue // table -> pk -> item
}

// Ensure MockDynamoDB satisfies the interface at compile time.
var _ metadb.DynamoDBAPI = (*MockDynamoDB)(nil)

// NewMockDynamoDB creates an empty in-memory DynamoDB mock.
func NewMockDynamoDB() *MockDynamoDB {
	return &MockDynamoDB{
		items: make(map[string]map[string]map[string]types.AttributeValue),
	}
}

// keyString builds a composite key from pk and sk values for in-memory lookup.
func keyString(pk, sk string) string {
	return pk + "|" + sk
}

// getItem retrieves an item map from the mock store.
func (m *MockDynamoDB) getItem(table, pk, sk string) (map[string]types.AttributeValue, bool) {
	tableItems, ok := m.items[table]
	if !ok {
		return nil, false
	}
	item, ok := tableItems[keyString(pk, sk)]
	return item, ok
}

// putItem stores an item in the mock store.
func (m *MockDynamoDB) putItem(table, pk, sk string, item map[string]types.AttributeValue) {
	if _, ok := m.items[table]; !ok {
		m.items[table] = make(map[string]map[string]types.AttributeValue)
	}
	m.items[table][keyString(pk, sk)] = item
}

// deleteItem removes an item from the mock store.
func (m *MockDynamoDB) deleteItem(table, pk, sk string) {
	if tableItems, ok := m.items[table]; ok {
		delete(tableItems, keyString(pk, sk))
	}
}

// queryByPrefix returns all items in a table where pk matches and sk starts with prefix.
func (m *MockDynamoDB) queryByPrefix(table, pk, skPrefix string) []map[string]types.AttributeValue {
	var results []map[string]types.AttributeValue
	tableItems, ok := m.items[table]
	if !ok {
		return results
	}
	for key, item := range tableItems {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		itemPK, itemSK := parts[0], parts[1]
		if itemPK == pk && strings.HasPrefix(itemSK, skPrefix) {
			results = append(results, item)
		}
	}
	return results
}

// ==============================================================================
// PutItem
// ==============================================================================

func (m *MockDynamoDB) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	table := aws.ToString(params.TableName)

	// Extract pk and sk from the item.
	pkAV, ok := params.Item["pk"]
	if !ok {
		return nil, fmt.Errorf("item missing pk attribute")
	}
	skAV, ok := params.Item["sk"]
	if !ok {
		return nil, fmt.Errorf("item missing sk attribute")
	}
	pk := pkAV.(*types.AttributeValueMemberS).Value
	sk := skAV.(*types.AttributeValueMemberS).Value

	// Evaluate condition expression.
	if params.ConditionExpression != nil {
		_, exists := m.getItem(table, pk, sk)
		expr := aws.ToString(params.ConditionExpression)
		if expr == "attribute_not_exists(pk)" && exists {
			return nil, &types.ConditionalCheckFailedException{
				Message: aws.String("The conditional request failed"),
			}
		}
	}

	// Deep-copy the item for storage.
	stored := make(map[string]types.AttributeValue, len(params.Item))
	for k, v := range params.Item {
		stored[k] = v
	}
	m.putItem(table, pk, sk, stored)
	return &dynamodb.PutItemOutput{}, nil
}

// ==============================================================================
// GetItem
// ==============================================================================

func (m *MockDynamoDB) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	table := aws.ToString(params.TableName)
	pk := params.Key["pk"].(*types.AttributeValueMemberS).Value
	sk := params.Key["sk"].(*types.AttributeValueMemberS).Value

	item, exists := m.getItem(table, pk, sk)
	if !exists {
		return &dynamodb.GetItemOutput{}, nil
	}
	// Return a copy.
	return &dynamodb.GetItemOutput{Item: item}, nil
}

// ==============================================================================
// UpdateItem
// ==============================================================================

func (m *MockDynamoDB) UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	table := aws.ToString(params.TableName)
	pk := params.Key["pk"].(*types.AttributeValueMemberS).Value
	sk := params.Key["sk"].(*types.AttributeValueMemberS).Value

	existing, exists := m.getItem(table, pk, sk)

	// Evaluate condition expression.
	if params.ConditionExpression != nil {
		expr := aws.ToString(params.ConditionExpression)
		if expr == "attribute_exists(pk)" && !exists {
			return nil, &types.ConditionalCheckFailedException{
				Message: aws.String("The conditional request failed"),
			}
		}
		// For more complex conditions (e.g., "#disabled <> :t"), check against existing item.
		if strings.Contains(expr, "#disabled <> :t") && exists {
			disabledAV, ok := existing["disabled"]
			if ok {
				disabled := disabledAV.(*types.AttributeValueMemberBOOL).Value
				if disabled {
					return nil, &types.ConditionalCheckFailedException{
						Message: aws.String("The conditional request failed"),
					}
				}
			}
		}
	}

	if !exists {
		// If condition didn't fail but item doesn't exist, create a new one
		// (DynamoDB UpdateItem can create if no condition).
		existing = make(map[string]types.AttributeValue)
	}

	// Apply update expression (simple SET clause).
	if params.UpdateExpression != nil {
		expr := aws.ToString(params.UpdateExpression)
		_ = applyUpdateExpression(existing, expr, params.ExpressionAttributeNames, params.ExpressionAttributeValues)
	}

	// Ensure pk/sk are set.
	existing["pk"] = params.Key["pk"]
	existing["sk"] = params.Key["sk"]

	m.putItem(table, pk, sk, existing)
	return &dynamodb.UpdateItemOutput{}, nil
}

// applyUpdateExpression evaluates a simple SET expression against an item.
// Supports: SET attr = :val, attr = :val, attr = :val
// Also supports: SET #name = :val format with ExpressionAttributeNames.
func applyUpdateExpression(item map[string]types.AttributeValue, expr string, names map[string]string, values map[string]types.AttributeValue) error {
	// Strip "SET " prefix.
	setClause := strings.TrimPrefix(expr, "SET ")
	parts := strings.Split(setClause, ", ")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		eq := strings.SplitN(part, " = ", 2)
		if len(eq) != 2 {
			continue
		}
		attrExpr := strings.TrimSpace(eq[0])
		valExpr := strings.TrimSpace(eq[1])

		// Resolve attribute name (e.g., "#disabled" → "disabled").
		attrName := attrExpr
		if strings.HasPrefix(attrExpr, "#") && names != nil {
			if resolved, ok := names[attrExpr]; ok {
				attrName = resolved
			}
		}

		// Resolve value reference (e.g., ":val" → actual value).
		if strings.HasPrefix(valExpr, ":") && values != nil {
			if val, ok := values[valExpr]; ok {
				item[attrName] = val
			}
		}
	}
	return nil
}

// ==============================================================================
// DeleteItem
// ==============================================================================

func (m *MockDynamoDB) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	table := aws.ToString(params.TableName)
	pk := params.Key["pk"].(*types.AttributeValueMemberS).Value
	sk := params.Key["sk"].(*types.AttributeValueMemberS).Value

	if params.ConditionExpression != nil {
		_, exists := m.getItem(table, pk, sk)
		if !exists {
			return nil, &types.ConditionalCheckFailedException{
				Message: aws.String("The conditional request failed"),
			}
		}
	}

	m.deleteItem(table, pk, sk)
	return &dynamodb.DeleteItemOutput{}, nil
}

// ==============================================================================
// Query
// ==============================================================================

func (m *MockDynamoDB) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	table := aws.ToString(params.TableName)
	cond := aws.ToString(params.KeyConditionExpression)

	// Simple expression parser: pk = :pk AND begins_with(sk, :prefix)
	// or: upload_id = :uid (GSI query)
	var results []map[string]types.AttributeValue

	if strings.Contains(cond, "begins_with") {
		// Extract pk and prefix values.
		pkVal := ""
		prefixVal := ""
		if val, ok := params.ExpressionAttributeValues[":pk"]; ok {
			pkVal = val.(*types.AttributeValueMemberS).Value
		}
		if val, ok := params.ExpressionAttributeValues[":prefix"]; ok {
			prefixVal = val.(*types.AttributeValueMemberS).Value
		}
		results = m.queryByPrefix(table, pkVal, prefixVal)
	} else if strings.Contains(cond, "upload_id") {
		// GSI query: upload_id = :uid
		uidVal := ""
		if val, ok := params.ExpressionAttributeValues[":uid"]; ok {
			uidVal = val.(*types.AttributeValueMemberS).Value
		}
		// Search all items for matching upload_id attribute.
		tableItems, ok := m.items[table]
		if ok {
			for _, item := range tableItems {
				if uidAV, ok := item["upload_id"]; ok {
					if uidAV.(*types.AttributeValueMemberS).Value == uidVal {
						results = append(results, item)
					}
				}
			}
		}
	}

	// Apply ScanIndexForward ordering: false = descending SK.
	scanForward := true
	if params.ScanIndexForward != nil {
		scanForward = *params.ScanIndexForward
	}
	if !scanForward {
		// Reverse results for descending order.
		for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
			results[i], results[j] = results[j], results[i]
		}
	}

	return &dynamodb.QueryOutput{Items: results}, nil
}

// ==============================================================================
// TransactWriteItems
// ==============================================================================

func (m *MockDynamoDB) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// For the mock, we evaluate all conditions first, then apply all writes.
	// This isn't a fully serialisable transaction but catches the main
	// failure modes (missing items, disabled items) in tests.

	// Phase 1: evaluate conditions.
	for _, item := range params.TransactItems {
		if item.Update != nil {
			upd := item.Update
			table := aws.ToString(upd.TableName)
			pk := upd.Key["pk"].(*types.AttributeValueMemberS).Value
			sk := upd.Key["sk"].(*types.AttributeValueMemberS).Value
			existing, exists := m.getItem(table, pk, sk)

			if upd.ConditionExpression != nil {
				expr := aws.ToString(upd.ConditionExpression)
				if expr == "attribute_exists(pk)" && !exists {
					return nil, fmt.Errorf("TransactionCanceledException: %w",
						&types.TransactionCanceledException{
							Message: aws.String("condition failed: attribute_exists(pk)"),
						})
				}
				if strings.Contains(expr, "#disabled <> :t") && exists {
					if disabledAV, ok := existing["disabled"]; ok {
						if disabledAV.(*types.AttributeValueMemberBOOL).Value {
							return nil, fmt.Errorf("TransactionCanceledException: %w",
								&types.TransactionCanceledException{
									Message: aws.String("condition failed: disabled is true"),
								})
						}
					}
				}
			}
		}
	}

	// Phase 2: apply writes.
	for _, item := range params.TransactItems {
		if item.Update != nil {
			upd := item.Update
			table := aws.ToString(upd.TableName)
			pk := upd.Key["pk"].(*types.AttributeValueMemberS).Value
			sk := upd.Key["sk"].(*types.AttributeValueMemberS).Value

			existing, exists := m.getItem(table, pk, sk)
			if !exists {
				existing = make(map[string]types.AttributeValue)
				existing["pk"] = upd.Key["pk"]
				existing["sk"] = upd.Key["sk"]
			}

			if upd.UpdateExpression != nil {
				_ = applyUpdateExpression(existing, aws.ToString(upd.UpdateExpression), upd.ExpressionAttributeNames, upd.ExpressionAttributeValues)
			}

			m.putItem(table, pk, sk, existing)
		}
	}

	return &dynamodb.TransactWriteItemsOutput{}, nil
}
