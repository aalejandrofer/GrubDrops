package twitch

import "encoding/json"

// gqlRequest is the JSON sent to gql.twitch.tv/gql for persisted queries.
type gqlRequest struct {
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables,omitempty"`
	Extensions    gqlExtensions  `json:"extensions"`
}

type gqlExtensions struct {
	PersistedQuery gqlPersistedQuery `json:"persistedQuery"`
}

type gqlPersistedQuery struct {
	Version    int    `json:"version"`
	Sha256Hash string `json:"sha256Hash"`
}

type gqlError struct {
	Message string `json:"message"`
	Path    []any  `json:"path,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors,omitempty"`
}
