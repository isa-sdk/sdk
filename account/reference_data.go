package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const (
	referenceV1Path = "/v1/reference-data"
	referenceV2Path = "/v2/reference-data"
	datasetPrefix   = "/dataset/"
)

// ReferenceDataInput is the request shape for ReferenceData.Get.
// Scope routes the call:
//
//   - "dataset"           → GET /dataset/{Dataset}
//   - "compiled_data_v3"  → POST /v2/reference-data
//   - anything else       → POST /v1/reference-data
//
// Payload is the optional caller-supplied filter document forwarded as
// the POST body alongside the scope field.
type ReferenceDataInput struct {
	Scope   string
	Dataset string
	Payload map[string]any
}

// ReferenceDataResult is the opaque response document. Callers
// down-cast; common shape is `{datasets: {...}}`.
type ReferenceDataResult map[string]any

// ReferenceDataService is the `account.referenceData` facade.
type ReferenceDataService struct {
	client *Client
}

// Get dispatches to the right reference-data endpoint based on Scope
// and returns the verbatim server response (envelope unwrapped).
func (s *ReferenceDataService) Get(ctx context.Context, scope string, opts ...ReferenceDataOption) (ReferenceDataResult, error) {
	if scope == "" {
		return nil, errors.New("account: ReferenceData.Get requires a non-empty scope")
	}
	in := ReferenceDataInput{Scope: scope}
	for _, opt := range opts {
		if opt != nil {
			opt(&in)
		}
	}
	if scope == "dataset" {
		return s.getDataset(ctx, in)
	}
	return s.postReference(ctx, in)
}

// ReferenceDataOption customizes one ReferenceData.Get call.
type ReferenceDataOption func(*ReferenceDataInput)

// WithDataset sets the dataset name. Required when Scope is "dataset".
func WithDataset(name string) ReferenceDataOption {
	return func(i *ReferenceDataInput) { i.Dataset = name }
}

// WithPayload sets the caller-supplied filter/parameter payload.
func WithPayload(p map[string]any) ReferenceDataOption {
	return func(i *ReferenceDataInput) { i.Payload = p }
}

func (s *ReferenceDataService) getDataset(ctx context.Context, in ReferenceDataInput) (ReferenceDataResult, error) {
	if in.Dataset == "" {
		return nil, errors.New("account: ReferenceData.Get requires Dataset when Scope is 'dataset'")
	}
	path := datasetPrefix + url.PathEscape(in.Dataset)
	body, err := s.client.signedDo(ctx, callArgs{method: http.MethodGet, path: path})
	if err != nil {
		return nil, fmt.Errorf("account: ReferenceData.Get dataset: %w", err)
	}
	return parseReference(body)
}

func (s *ReferenceDataService) postReference(ctx context.Context, in ReferenceDataInput) (ReferenceDataResult, error) {
	path := referenceV1Path
	if in.Scope == "compiled_data_v3" {
		path = referenceV2Path
	}
	wire := map[string]any{}
	for k, v := range in.Payload {
		if k == "scope" {
			continue
		}
		wire[k] = v
	}
	wire["scope"] = in.Scope
	bodyBytes, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("account: ReferenceData.Get marshal: %w", err)
	}
	body, err := s.client.signedDo(ctx, callArgs{
		method: http.MethodPost,
		path:   path,
		body:   bodyBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("account: ReferenceData.Get: %w", err)
	}
	return parseReference(body)
}

func parseReference(body []byte) (ReferenceDataResult, error) {
	if len(body) == 0 {
		return ReferenceDataResult{}, nil
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, fmt.Errorf("account: ReferenceData envelope: %w", err)
	}
	if len(data) == 0 {
		return ReferenceDataResult{}, nil
	}
	out := ReferenceDataResult{}
	if err := json.Unmarshal(data, &out); err != nil {
		// Fallback: wrap non-object payload under "data".
		return ReferenceDataResult{"data": json.RawMessage(data)}, nil
	}
	return out, nil
}
