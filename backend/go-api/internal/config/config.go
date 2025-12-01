// ==============================================================================
// config.go — Environment-driven configuration for the backend API
//
// All configuration is loaded from environment variables, following the
// twelve-factor app pattern.  No config files, no command-line flags.
//
// Sensible defaults are provided for local development; production values
// are injected by Terraform via Lambda environment variables.
// ==============================================================================

package config

import (
	"os"
	"strconv"
)

// Config holds all configuration for the backend API service.
type Config struct {
	// AWS
	Region        string // AWS region (e.g., us-east-1)
	SitesBucket   string // S3 bucket for staging and published content
	S3Endpoint    string // Optional custom S3 endpoint (empty = use default AWS endpoint)

	// DynamoDB
	SiteMetadataTable  string // Site metadata table (users, sites, host mappings)
	UploadRecordsTable string // Upload records table (upload lifecycle tracking)
	DynamoDBEndpoint   string // Optional custom DynamoDB endpoint (empty = use default AWS endpoint)

	// Cognito
	CognitoUserPoolID string // Cognito user pool ID (for JWKS verification in production)

	// Routing
	SitesDomain  string // Base domain for hosted sites (e.g., sites.example.com)
	AliasEnabled bool   // Whether the compatibility alias hostname pattern is enabled

	// Server (local development mode)
	Port string // HTTP listen port for local development server

	// Logging
	LogLevel string // Log level: debug, info, warn, error
}

// Load reads configuration from environment variables and returns a Config
// with sensible defaults for local development.
func Load() *Config {
	return &Config{
		Region:             envOrDefault("AWS_REGION", "us-east-1"),
		SitesBucket:        envOrDefault("SITES_BUCKET", ""),
		SiteMetadataTable:  envOrDefault("SITE_METADATA_TABLE", "site_metadata"),
		UploadRecordsTable: envOrDefault("UPLOAD_RECORDS_TABLE", "upload_records"),
		DynamoDBEndpoint:   os.Getenv("DYNAMODB_ENDPOINT"),
		S3Endpoint:         os.Getenv("S3_ENDPOINT"),
		CognitoUserPoolID:  envOrDefault("COGNITO_USER_POOL_ID", ""),
		SitesDomain:        envOrDefault("SITES_DOMAIN", "sites.example.com"),
		AliasEnabled:       envBoolOrDefault("ALIAS_ENABLED", true),
		Port:               envOrDefault("PORT", "8080"),
		LogLevel:           envOrDefault("LOG_LEVEL", "info"),
	}
}

// envOrDefault returns the value of the named environment variable, or the
// default value if the variable is not set or empty.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envBoolOrDefault returns the boolean value of the named environment variable,
// or the default value if the variable is not set or cannot be parsed.
func envBoolOrDefault(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}
