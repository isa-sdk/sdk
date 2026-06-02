package zyins

import (
	"context"
	"errors"
	"testing"

	"github.com/isa-sdk/sdk/zyins/cases"
)

// mockCaseStorage is an in-memory [cases.CaseStorage] used to verify
// the CasesService.Save / CasesService.Open / CasesService.Delete
// surfaces delegate to the configured storage.
type mockCaseStorage struct {
	records map[string]cases.CaseRecord
}

func newMockCaseStorage() *mockCaseStorage {
	return &mockCaseStorage{records: make(map[string]cases.CaseRecord)}
}

func (m *mockCaseStorage) Put(_ context.Context, record cases.CaseRecord) (cases.PutResult, error) {
	id := record.ID
	if id == "" {
		id = "mock_" + record.Product
	}
	stored := record
	stored.ID = id
	m.records[id] = stored
	return cases.PutResult{ID: id, RecallToken: "mock-token-" + id}, nil
}

func (m *mockCaseStorage) Get(_ context.Context, id, recallToken string) (cases.CaseRecord, error) {
	rec, ok := m.records[id]
	if !ok {
		return cases.CaseRecord{}, cases.ErrNotFound
	}
	if recallToken != "mock-token-"+id {
		return cases.CaseRecord{}, cases.ErrNotFound
	}
	return rec, nil
}

// deleteableMockCaseStorage adds the optional Delete capability.
type deleteableMockCaseStorage struct {
	*mockCaseStorage
}

func (m deleteableMockCaseStorage) Delete(_ context.Context, id string) error {
	delete(m.records, id)
	return nil
}

func newCaseStorageTestClient(t *testing.T, opts ...Option) *Client {
	t.Helper()
	allOpts := append([]Option{WithToken("isa_test_aaaaaaaaaaaaaaaaaaaa")}, opts...)
	c, err := NewClient(allOpts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestCasesService_Save_DelegatesToConfiguredStorage(t *testing.T) {
	mock := newMockCaseStorage()
	c := newCaseStorageTestClient(t, WithCaseStorage(mock))

	put, err := c.Cases.Save(context.Background(), cases.CaseRecord{
		Product: "zyins",
		Body:    []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if put.ID != "mock_zyins" {
		t.Errorf("ID = %q, want mock_zyins", put.ID)
	}

	got, err := c.Cases.Open(context.Background(), put.ID, put.RecallToken)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got.Body) != "payload" {
		t.Errorf("body = %q, want payload", got.Body)
	}
}

func TestCasesService_Delete_UnsupportedStorageReturnsErrUnsupported(t *testing.T) {
	mock := newMockCaseStorage() // does NOT implement DeleteableCaseStorage
	c := newCaseStorageTestClient(t, WithCaseStorage(mock))

	err := c.Cases.Delete(context.Background(), "any_id")
	if !errors.Is(err, cases.ErrUnsupported) {
		t.Errorf("Delete on non-deleteable storage: want ErrUnsupported, got %v", err)
	}
}

func TestCasesService_Delete_DeleteableStorageHonored(t *testing.T) {
	deleteable := deleteableMockCaseStorage{mockCaseStorage: newMockCaseStorage()}
	c := newCaseStorageTestClient(t, WithCaseStorage(deleteable))

	put, err := c.Cases.Save(context.Background(), cases.CaseRecord{Product: "zyins", Body: []byte("x")})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := c.Cases.Delete(context.Background(), put.ID); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if _, err := c.Cases.Open(context.Background(), put.ID, put.RecallToken); !errors.Is(err, cases.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCasesService_Save_DefaultIsZeroKnowledge(t *testing.T) {
	c := newCaseStorageTestClient(t)
	storage := c.resolveCaseStorage()
	if storage == nil {
		t.Fatal("default storage nil")
	}
	// Calling twice returns the same instance (lazy memoization).
	if c.resolveCaseStorage() != storage {
		t.Error("default storage not memoized")
	}
}

func TestClient_APIVersionFor_HonorsOverrides(t *testing.T) {
	c := newCaseStorageTestClient(t, mustOption(t, WithAPIVersionOverrides(map[string]string{
		"quote": "v2",
	})))
	if got := c.APIVersionFor("quote"); got != "v2" {
		t.Errorf("APIVersionFor(quote) = %q, want v2 (override)", got)
	}
	if got := c.APIVersionFor("prequalify"); got != "v3" {
		t.Errorf("APIVersionFor(prequalify) = %q, want v3 (bundled)", got)
	}
	if got := c.APIVersionFor("totally_unknown"); got != "" {
		t.Errorf("APIVersionFor(unknown) = %q, want \"\"", got)
	}
}

func TestClient_APIVersionFor_NoOverridesUsesBundled(t *testing.T) {
	c := newCaseStorageTestClient(t)
	if got := c.APIVersionFor("branding"); got != "v1" {
		t.Errorf("APIVersionFor(branding) = %q, want v1", got)
	}
}

func TestWithAPIVersionOverrides_RejectsEmptyValues(t *testing.T) {
	_, err := NewClient(
		WithToken("isa_test_aaaaaaaaaaaaaaaaaaaa"),
		WithAPIVersionOverrides(map[string]string{"quote": ""}),
	)
	if err == nil {
		t.Fatal("expected error for empty version override")
	}
}

func mustOption(t *testing.T, opt Option) Option {
	t.Helper()
	return opt
}
