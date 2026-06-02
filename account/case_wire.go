package account

import (
	"encoding/json"
	"fmt"
)

// Wire encode/decode for the zero-knowledge case endpoints: the create
// request body and the create / detail response shapes. Kept apart from the
// link + crypto logic so each file stays focused.

// marshalShareBody serializes the POST /v1/case body: the cleartext product
// tag alongside the opaque envelope fields.
func marshalShareBody(product string, env CaseEnvelope) ([]byte, error) {
	body, err := json.Marshal(struct {
		Product    string `json:"product"`
		Ciphertext string `json:"ciphertext"`
		IV         string `json:"iv"`
		Tag        string `json:"tag"`
	}{product, env.Ciphertext, env.IV, env.Tag})
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Share marshal body: %w", err)
	}
	return body, nil
}

// parseSharedCaseID decodes a create response into the server-assigned id.
func parseSharedCaseID(body []byte) (string, error) {
	root, err := caseRecord(body, "Cases.Share")
	if err != nil {
		return "", err
	}
	return requiredCaseString(root, "id", "Cases.Share")
}

// parseCaseDetail decodes a GET /v1/case/{code} response into its product tag
// and opaque envelope.
func parseCaseDetail(body []byte) (string, CaseEnvelope, error) {
	root, err := caseRecord(body, "Cases.Open")
	if err != nil {
		return "", CaseEnvelope{}, err
	}
	product, err := requiredCaseString(root, "product", "Cases.Open")
	if err != nil {
		return "", CaseEnvelope{}, err
	}
	ciphertext, err := requiredCaseString(root, "ciphertext", "Cases.Open")
	if err != nil {
		return "", CaseEnvelope{}, err
	}
	iv, err := requiredCaseString(root, "iv", "Cases.Open")
	if err != nil {
		return "", CaseEnvelope{}, err
	}
	tag, err := requiredCaseString(root, "tag", "Cases.Open")
	if err != nil {
		return "", CaseEnvelope{}, err
	}
	return product, CaseEnvelope{Ciphertext: ciphertext, IV: iv, Tag: tag}, nil
}

// caseRecord unwraps the response envelope and parses the data payload into a
// string-keyed record, with operation-tagged errors.
func caseRecord(body []byte, operation string) (map[string]json.RawMessage, error) {
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, fmt.Errorf("account: %s envelope: %w", operation, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("account: %s response body was empty", operation)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("account: %s response body was not a JSON object: %w", operation, err)
	}
	return root, nil
}

// requiredCaseString extracts a non-empty string field, erroring when absent.
func requiredCaseString(root map[string]json.RawMessage, key, operation string) (string, error) {
	raw, ok := root[key]
	if !ok {
		return "", fmt.Errorf("account: %s response body is missing %q", operation, key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("account: %s field %q was not a string: %w", operation, key, err)
	}
	if value == "" {
		return "", fmt.Errorf("account: %s response body has empty %q", operation, key)
	}
	return value, nil
}
