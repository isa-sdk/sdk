package zyins

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBuildDatasetsV3Query_EncodesReservedCharacters(t *testing.T) {
	query := buildDatasetsV3Query(DatasetsV3Options{
		Include: []DatasetCategory{DatasetCategoryConditions, DatasetCategoryMedications},
		Fields:  "meta,items",
	})
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if got := values.Get("include"); got != "conditions,medications" {
		t.Errorf("include = %q, want %q", got, "conditions,medications")
	}
	if got := values.Get("fields"); got != "meta,items" {
		t.Errorf("fields = %q, want %q", got, "meta,items")
	}
}

func TestBuildDatasetsV3Query_EmptyIncludeIsExplicit(t *testing.T) {
	values, err := url.ParseQuery(buildDatasetsV3Query(DatasetsV3Options{
		Include: []DatasetCategory{},
	}))
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if _, ok := values["include"]; !ok {
		t.Fatal("expected explicit empty include query parameter")
	}
}

func TestDatasetsV3Get_NotModifiedPreservesRequestETag(t *testing.T) {
	const cachedETag = `"cached"`
	doer := &logoFakeDoer{resp: &http.Response{
		StatusCode: http.StatusNotModified,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
	}}
	client, err := NewClient(WithToken("isa_test_abc"), WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.doer = doer

	got, err := client.DatasetsV3.Get(context.Background(), DatasetsV3Options{IfNoneMatch: cachedETag})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.NotModified {
		t.Fatal("expected NotModified")
	}
	if got.ETag != cachedETag {
		t.Errorf("ETag = %q, want %q", got.ETag, cachedETag)
	}
}

func TestDecodeDatasetsV3Envelope_SurfacesProductSlices(t *testing.T) {
	// A3: products_by_family / discontinued_products / state_derivatives
	// pass through as typed fields. A row is kept when its id is non-empty;
	// a blank or missing name keeps the row (Name=""), while a row with no
	// id is dropped — matching the TS/Python/PHP/C# keep predicate.
	body := []byte(`{"data":{
		"catalog_version":"3.0",
		"products_by_family":{"final_expense":[
			{"id":"prod_001","name":"Mountain Life MYGA"},
			{"id":"prod_002"},
			{"name":"orphan"}
		]},
		"discontinued_products":{"mountain-life-myga":1746979200},
		"state_derivatives":["ND","SD"]
	}}`)
	bundle, err := decodeDatasetsV3Envelope(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	fam := bundle.ProductsByFamily["final_expense"]
	wantFam := []ReferenceEntity{
		{ID: "prod_001", Name: "Mountain Life MYGA"},
		{ID: "prod_002", Name: ""},
	}
	if len(fam) != len(wantFam) || fam[0] != wantFam[0] || fam[1] != wantFam[1] {
		t.Errorf("ProductsByFamily[final_expense] = %+v, want %+v (id-only row kept, orphan dropped)", fam, wantFam)
	}
	if bundle.DiscontinuedProducts["mountain-life-myga"] != 1746979200 {
		t.Errorf("DiscontinuedProducts = %+v", bundle.DiscontinuedProducts)
	}
	want := []string{"ND", "SD"}
	if len(bundle.StateDerivatives) != 2 || bundle.StateDerivatives[0] != want[0] || bundle.StateDerivatives[1] != want[1] {
		t.Errorf("StateDerivatives = %v, want %v", bundle.StateDerivatives, want)
	}
}

func TestDecodeDatasetsV3Envelope_MalformedSliceElementsSkipNotAbort(t *testing.T) {
	// A single non-integer epoch, non-string derivative, or empty-id row
	// must skip only that element — never abort the whole bundle decode.
	// This mirrors the lenient TS/Python/PHP/C# parsers; a strict typed
	// json.Unmarshal would instead return an UnmarshalTypeError here and
	// leave the consumer with no bundle at all.
	body := []byte(`{"data":{
		"catalog_version":"3.0",
		"products_by_family":{"final_expense":[
			{"id":"prod_001","name":"Mountain Life MYGA"},
			{"id":"","name":"empty id skipped"},
			42,
			"not-an-object"
		]},
		"discontinued_products":{
			"mountain-life-myga":1746979200,
			"float-epoch-ok":1746979200.0,
			"sci-epoch-ok":1.7e9,
			"fractional-skipped":1746979200.5,
			"string-skipped":"not-a-number",
			"bool-skipped":true
		},
		"state_derivatives":["ND",7,null,"SD"]
	}}`)
	bundle, err := decodeDatasetsV3Envelope(body)
	if err != nil {
		t.Fatalf("decode must not fail on malformed slice elements: %v", err)
	}

	fam := bundle.ProductsByFamily["final_expense"]
	if len(fam) != 1 || fam[0].ID != "prod_001" {
		t.Errorf("ProductsByFamily[final_expense] = %+v, want only the prod_001 row", fam)
	}

	want := map[string]int64{
		"mountain-life-myga": 1746979200,
		"float-epoch-ok":     1746979200,
		"sci-epoch-ok":       1700000000,
	}
	if len(bundle.DiscontinuedProducts) != len(want) {
		t.Errorf("DiscontinuedProducts = %+v, want %+v", bundle.DiscontinuedProducts, want)
	}
	for slug, epoch := range want {
		if bundle.DiscontinuedProducts[slug] != epoch {
			t.Errorf("DiscontinuedProducts[%q] = %d, want %d", slug, bundle.DiscontinuedProducts[slug], epoch)
		}
	}

	gotStates := []string{"ND", "SD"}
	if len(bundle.StateDerivatives) != len(gotStates) || bundle.StateDerivatives[0] != gotStates[0] || bundle.StateDerivatives[1] != gotStates[1] {
		t.Errorf("StateDerivatives = %v, want %v", bundle.StateDerivatives, gotStates)
	}
}

func TestDecodeDatasetsV3Envelope_KeepsBlankNameAndBlankDerivative(t *testing.T) {
	// Cross-language parity guard. The keep predicate for a product row is
	// "non-empty id, name is a string" — a blank display name does NOT drop
	// the row. State derivatives keep every string element, including the
	// empty string. TS/Python/PHP/C# all behave this way; before this guard
	// the Go parser uniquely dropped blank-name rows and blank derivatives,
	// so the same wire payload produced fewer rows on Go clients alone.
	body := []byte(`{"data":{
		"catalog_version":"3.0",
		"products_by_family":{"final_expense":[
			{"id":"prod_001","name":"Mountain Life MYGA"},
			{"id":"prod_blank_name","name":""},
			{"id":"prod_no_name"}
		]},
		"state_derivatives":["ND","",null,"SD"]
	}}`)
	bundle, err := decodeDatasetsV3Envelope(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	fam := bundle.ProductsByFamily["final_expense"]
	wantRows := []ReferenceEntity{
		{ID: "prod_001", Name: "Mountain Life MYGA"},
		{ID: "prod_blank_name", Name: ""},
		{ID: "prod_no_name", Name: ""},
	}
	if len(fam) != len(wantRows) {
		t.Fatalf("ProductsByFamily[final_expense] = %+v, want %d rows incl. blank-name", fam, len(wantRows))
	}
	for i, want := range wantRows {
		if fam[i] != want {
			t.Errorf("ProductsByFamily[final_expense][%d] = %+v, want %+v", i, fam[i], want)
		}
	}

	// "" is a string element and is kept; null is not a string and is
	// dropped — exactly the other four parsers' behavior.
	wantStates := []string{"ND", "", "SD"}
	if len(bundle.StateDerivatives) != len(wantStates) {
		t.Fatalf("StateDerivatives = %#v, want %#v (empty string kept, null dropped)", bundle.StateDerivatives, wantStates)
	}
	for i, want := range wantStates {
		if bundle.StateDerivatives[i] != want {
			t.Errorf("StateDerivatives[%d] = %q, want %q", i, bundle.StateDerivatives[i], want)
		}
	}
}

func TestDecodeDatasetsV3Envelope_OutOfRangeEpochSkipped(t *testing.T) {
	// Cross-language parity guard for the int64 epoch bound. An epoch that
	// overflows int64 must skip only that entry — never be kept as a wrapped
	// or imprecise value. Go, C#, PHP (int64-typed) and Python/TS (range-gated)
	// all drop the out-of-range entry and keep the in-range one. 9.3e18 > 2^63.
	body := []byte(`{"data":{
		"catalog_version":"3.0",
		"discontinued_products":{
			"in-range":1746979200,
			"overflow-skipped":9300000000000000000,
			"overflow-float-skipped":9.3e18
		}
	}}`)
	bundle, err := decodeDatasetsV3Envelope(body)
	if err != nil {
		t.Fatalf("decode must not fail on out-of-range epoch: %v", err)
	}
	if _, ok := bundle.DiscontinuedProducts["overflow-skipped"]; ok {
		t.Errorf("overflow-skipped should be dropped, got %d", bundle.DiscontinuedProducts["overflow-skipped"])
	}
	if _, ok := bundle.DiscontinuedProducts["overflow-float-skipped"]; ok {
		t.Errorf("overflow-float-skipped should be dropped, got %d", bundle.DiscontinuedProducts["overflow-float-skipped"])
	}
	if bundle.DiscontinuedProducts["in-range"] != 1746979200 {
		t.Errorf("in-range = %d, want 1746979200", bundle.DiscontinuedProducts["in-range"])
	}
	if len(bundle.DiscontinuedProducts) != 1 {
		t.Errorf("DiscontinuedProducts = %+v, want only the in-range entry", bundle.DiscontinuedProducts)
	}
}

func assertEmptyPresentSlices(t *testing.T, bundle *DatasetBundleV3, context string) {
	t.Helper()
	if bundle.ProductsByFamily == nil {
		t.Errorf("%s: ProductsByFamily = nil, want non-nil empty map", context)
	}
	if len(bundle.ProductsByFamily) != 0 {
		t.Errorf("%s: ProductsByFamily = %v, want empty", context, bundle.ProductsByFamily)
	}
	if bundle.DiscontinuedProducts == nil {
		t.Errorf("%s: DiscontinuedProducts = nil, want non-nil empty map", context)
	}
	if len(bundle.DiscontinuedProducts) != 0 {
		t.Errorf("%s: DiscontinuedProducts = %v, want empty", context, bundle.DiscontinuedProducts)
	}
	if bundle.StateDerivatives == nil {
		t.Errorf("%s: StateDerivatives = nil, want non-nil empty slice", context)
	}
	if len(bundle.StateDerivatives) != 0 {
		t.Errorf("%s: StateDerivatives = %v, want empty", context, bundle.StateDerivatives)
	}
}

func TestDecodeDatasetsV3Envelope_SlicesAlwaysPresentNeverNil(t *testing.T) {
	// Cross-language parity guard. The product slices surface as a non-nil
	// (possibly empty) collection in every case — absent, explicitly-null, AND
	// explicitly-empty all yield a present empty map/slice, never nil. The
	// TS/Python/PHP parsers all return a present empty collection for the
	// omitted case (a nil/null here marshals to JSON `null` and diverges from
	// the `{}`/`[]` the others emit), so this is the canonical five-language
	// contract; Go previously returned nil for omitted and diverged.
	cases := []struct {
		name string
		body string
	}{
		{"omitted", `{"data":{"catalog_version":"3.0"}}`},
		{"explicit null", `{"data":{"catalog_version":"3.0","products_by_family":null,"discontinued_products":null,"state_derivatives":null}}`},
		{"explicit empty", `{"data":{"catalog_version":"3.0","products_by_family":{},"discontinued_products":{},"state_derivatives":[]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := decodeDatasetsV3Envelope([]byte(tc.body))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			assertEmptyPresentSlices(t, bundle, tc.name)
		})
	}
}

func TestDecodeDatasetsV3Envelope_NonArrayFamilySkipped(t *testing.T) {
	// Cross-language parity guard. A products_by_family family whose value is
	// not a JSON array is skipped entirely — no phantom key is emitted — while
	// a family with an empty array [] is kept as an empty list. TS/Python/PHP
	// skip a non-array family; before this guard Go (and C#) emitted an
	// empty-list key for it and diverged.
	body := []byte(`{"data":{
		"catalog_version":"3.0",
		"products_by_family":{
			"final_expense":[{"id":"prod_001","name":"Mountain Life MYGA"}],
			"bad_number":42,
			"bad_string":"nope",
			"bad_object":{"id":"x"},
			"empty_kept":[]
		}
	}}`)
	bundle, err := decodeDatasetsV3Envelope(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := bundle.ProductsByFamily["bad_number"]; ok {
		t.Error("bad_number family (non-array) should be skipped, not keyed")
	}
	if _, ok := bundle.ProductsByFamily["bad_string"]; ok {
		t.Error("bad_string family (non-array) should be skipped, not keyed")
	}
	if _, ok := bundle.ProductsByFamily["bad_object"]; ok {
		t.Error("bad_object family (non-array) should be skipped, not keyed")
	}
	empty, ok := bundle.ProductsByFamily["empty_kept"]
	if !ok {
		t.Error("empty_kept family ([]) should be present as an empty list")
	}
	if len(empty) != 0 {
		t.Errorf("empty_kept = %+v, want empty list", empty)
	}
	fam := bundle.ProductsByFamily["final_expense"]
	if len(fam) != 1 || fam[0].ID != "prod_001" {
		t.Errorf("final_expense = %+v, want the single prod_001 row", fam)
	}
	if len(bundle.ProductsByFamily) != 2 {
		t.Errorf("ProductsByFamily has %d keys, want 2 (final_expense + empty_kept)", len(bundle.ProductsByFamily))
	}
}
