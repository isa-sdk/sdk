// Package zyins — GET /v3/datasets transport.
//
// The v3 datasets surface ships every row as a self-contained record
// (the standalone-licensable shape — every row IS the contract).
// `treated_with[]` lives on each condition row; `used_for[]` lives on
// each medication row. Spelling corrections and nicotine options ship
// the same way. The legacy maps-shape (`medications_by_condition`,
// `frequency_graphs.use_map`) is intentionally removed from the wire —
// the SDK derives the lookup indexes internally from the inline rows.
//
// See /tmp/v3-datasets-adapter-cutover-spec.md §1 for the contract.

package zyins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// datasetsV3Path is the canonical path for the v3 datasets read.
const datasetsV3Path = "/v3/datasets"

// DatasetsV3Options narrows the dataset request and supports
// conditional revalidation.
type DatasetsV3Options struct {
	// Include narrows the response to specific categories. nil requests
	// all categories. An explicit empty slice sends include= for the
	// server's meta-only shortcut.
	Include []DatasetCategory
	// Fields selects payload depth. "" (default) returns full rows;
	// "meta" returns per-category versions + item counts only.
	Fields string
	// IfNoneMatch forwards the caller's cached ETag as If-None-Match.
	// When the server returns 304, Get reports NotModified=true and
	// echoes the cached ETag.
	IfNoneMatch string
}

// DatasetsV3Result is the outcome of a GET /v3/datasets call. Exactly
// one of NotModified or Bundle is populated.
type DatasetsV3Result struct {
	// NotModified is true when the server returned 304 and the caller
	// should keep its prior bundle.
	NotModified bool
	// Bundle is the fresh catalog; nil when NotModified is true.
	Bundle *DatasetBundleV3
	// ETag is the response's ETag header, empty if not present.
	ETag string
}

// DatasetsV3Service implements GET /v3/datasets.
type DatasetsV3Service struct {
	client *Client
}

// Get fetches the typed v3 catalog.
func (s *DatasetsV3Service) Get(ctx context.Context, opts DatasetsV3Options) (*DatasetsV3Result, error) {
	path := datasetsV3Path
	if q := buildDatasetsV3Query(opts); q != "" {
		path = datasetsV3Path + "?" + q
	}
	extra := map[string]string{}
	if opts.IfNoneMatch != "" {
		extra["If-None-Match"] = opts.IfNoneMatch
	}
	raw, httpResp, err := s.client.doJSONRaw(ctx, requestArgs{
		method:       http.MethodGet,
		path:         path,
		op:           "datasets_v3_get",
		extraHeaders: extra,
	})
	if err != nil {
		if httpResp != nil && httpResp.StatusCode == http.StatusNotModified {
			etag := httpResp.Header.Get("ETag")
			if etag == "" {
				etag = opts.IfNoneMatch
			}
			return &DatasetsV3Result{
				NotModified: true,
				ETag:        etag,
			}, nil
		}
		return nil, fmt.Errorf("zyins: DatasetsV3.Get: %w", err)
	}
	bundle, err := decodeDatasetsV3Envelope(raw)
	if err != nil {
		return nil, fmt.Errorf("zyins: DatasetsV3.Get decode: %w", err)
	}
	etag := ""
	if httpResp != nil {
		etag = httpResp.Header.Get("ETag")
	}
	bundle.ETag = etag
	return &DatasetsV3Result{Bundle: bundle, ETag: etag}, nil
}

// buildDatasetsV3Query renders the GET query string.
func buildDatasetsV3Query(opts DatasetsV3Options) string {
	q := url.Values{}
	if opts.Include != nil {
		labels := make([]string, len(opts.Include))
		for i, c := range opts.Include {
			labels[i] = string(c)
		}
		q.Set("include", strings.Join(labels, ","))
	}
	if opts.Fields != "" {
		q.Set("fields", opts.Fields)
	}
	return q.Encode()
}

// ---------------------------------------------------------------------------
// Wire decoding — inline-row shape per spec §1.
// ---------------------------------------------------------------------------

// datasetsV3WireRelation is one entry inside the inline `treated_with`
// or `used_for` array.
type datasetsV3WireRelation struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	PrescriptionCount int    `json:"prescription_count"`
}

// datasetsV3WireRow is the union shape: every entity row carries id
// plus name; conditions add treated_with; medications add used_for;
// nicotine options add type; spelling corrections carry from/to.
type datasetsV3WireRow struct {
	Object            string                   `json:"object"`
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	TreatedWith       []datasetsV3WireRelation `json:"treated_with"`
	UsedFor           []datasetsV3WireRelation `json:"used_for"`
	Type              string                   `json:"type"`
	From              string                   `json:"from"`
	To                string                   `json:"to"`
	PrescriptionCount int                      `json:"prescription_count"`
}

type datasetsV3WireEntry struct {
	Version   string              `json:"version"`
	ItemCount *int                `json:"item_count"`
	Items     []datasetsV3WireRow `json:"items"`
}

type datasetsV3WireData struct {
	CatalogVersion string                                  `json:"catalog_version"`
	Version        string                                  `json:"version"`
	Datasets       map[DatasetCategory]datasetsV3WireEntry `json:"datasets"`
	// The three product-slice fields are decoded as raw JSON and parsed
	// per-element so one malformed entry (a non-integer epoch, a
	// non-string derivative) skips only that entry instead of aborting
	// the whole bundle decode — matching the lenient TS/Python/PHP/C#
	// mirrors.
	ProductsByFamily     json.RawMessage `json:"products_by_family"`
	DiscontinuedProducts json.RawMessage `json:"discontinued_products"`
	StateDerivatives     json.RawMessage `json:"state_derivatives"`
}

type datasetsV3WireEnvelope struct {
	Data json.RawMessage `json:"data"`
}

func decodeDatasetsV3Envelope(body []byte) (*DatasetBundleV3, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("zyins: datasets_v3 response body was empty")
	}
	var env datasetsV3WireEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode datasets_v3 envelope: %w", err)
	}
	target := body
	if len(env.Data) > 0 {
		if isJSONNull(env.Data) {
			return nil, fmt.Errorf("zyins: datasets_v3 returned null data envelope")
		}
		target = env.Data
	}
	var data datasetsV3WireData
	if err := json.Unmarshal(target, &data); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode datasets_v3 data: %w", err)
	}
	return datasetsV3FromWire(&data), nil
}

func datasetsV3FromWire(w *datasetsV3WireData) *DatasetBundleV3 {
	datasets := make(map[DatasetCategory]*DatasetEntry, len(w.Datasets))
	bundle := &DatasetBundleV3{
		Version: chooseVersion(w.CatalogVersion, w.Version),
	}
	for cat, entry := range w.Datasets {
		typedEntry := &DatasetEntry{
			Version: entry.Version,
			Items:   datasetsV3EntitiesFromWire(entry.Items),
		}
		if entry.ItemCount != nil {
			typedEntry.ItemCount = *entry.ItemCount
		} else {
			typedEntry.ItemCount = len(typedEntry.Items)
		}
		datasets[cat] = typedEntry
		switch cat {
		case DatasetCategoryConditions:
			bundle.Conditions = typedEntry.Items
			bundle.ConditionRelations = relationsFromConditionRows(entry.Items)
		case DatasetCategoryMedications:
			bundle.Medications = typedEntry.Items
			bundle.MedicationRelations = relationsFromMedicationRows(entry.Items)
		case DatasetCategoryNicotineOptions:
			bundle.NicotineOptions = nicotineOptionsFromRows(entry.Items)
		case datasetCategorySpellingCorrections:
			bundle.SpellingCorrections = spellingCorrectionsFromRows(entry.Items)
		case DatasetCategoryProducts:
			bundle.Products = typedEntry.Items
		case DatasetCategoryCorrections:
			// Legacy alias for spelling_corrections during the v3 rc.1 →
			// 1.0 cutover. The server may emit either; the SDK accepts
			// both.
			if bundle.SpellingCorrections == nil {
				bundle.SpellingCorrections = spellingCorrectionsFromRows(entry.Items)
			}
		}
	}
	bundle.Datasets = datasets
	bundle.ProductsByFamily = productsByFamilyFromWire(w.ProductsByFamily)
	bundle.DiscontinuedProducts = discontinuedProductsFromWire(w.DiscontinuedProducts)
	bundle.StateDerivatives = stateDerivativesFromWire(w.StateDerivatives)
	return bundle
}

// productsByFamilyFromWire projects each family's product rows into typed
// ReferenceEntity slices. A row is kept when its id is a non-empty string;
// a blank display name is preserved as the empty string rather than dropping
// the row. This matches the TS/Python/PHP/C# parsers, which all gate on
// non-empty id alone (the id is the stable lookup key; the name is a display
// label the server may legitimately leave blank).
//
// A family whose value is not a JSON array is skipped entirely — no phantom
// key is emitted. An empty array [] is a valid array and maps to an empty
// list. This matches the TS/Python/PHP parsers, which skip a non-array family
// rather than mapping it to an empty list.
//
// The result is always a non-nil (possibly empty) map: an absent, null, or
// explicitly-empty field all yield an empty map, never nil. The TS/Python/PHP
// parsers surface every case as a present empty collection, so returning nil
// for the omitted case would diverge. Consumers range over the map without a
// nil branch.
func productsByFamilyFromWire(raw json.RawMessage) map[string][]ReferenceEntity {
	families := decodeRawObject(raw)
	out := make(map[string][]ReferenceEntity, len(families))
	for family, value := range families {
		if !isJSONArray(value) {
			continue
		}
		rows := decodeRawArray(value)
		entities := make([]ReferenceEntity, 0, len(rows))
		for _, r := range rows {
			row := decodeRawProductRow(r)
			if row.ID == "" {
				continue
			}
			entities = append(entities, ReferenceEntity{ID: row.ID, Name: row.Name})
		}
		out[family] = entities
	}
	return out
}

// discontinuedProductsFromWire parses the slug → unix-epoch-second map,
// keeping only entries whose value is an integer-valued JSON number.
// Genuine fractional epochs (e.g. 1.5) are skipped; integer-valued
// numbers in any notation (170…, 1.7e9, 170….0) are kept and held as
// int64 to stay 2038-safe. One bad entry skips only itself, never the
// whole bundle.
//
// The result is always a non-nil (possibly empty) map — same always-present
// empty-collection contract as productsByFamilyFromWire, matching the
// TS/Python/PHP parsers. An absent, null, or explicitly-empty field all yield
// an empty map, never nil.
func discontinuedProductsFromWire(raw json.RawMessage) map[string]int64 {
	entries := decodeRawObject(raw)
	out := make(map[string]int64, len(entries))
	for slug, value := range entries {
		epoch, ok := integerEpochFromRaw(value)
		if !ok {
			continue
		}
		out[slug] = epoch
	}
	return out
}

// stateDerivativesFromWire parses the derivative-state slug list, keeping
// every JSON string element — including the empty string. Only non-string
// elements (null, numbers, objects) are skipped. This matches the
// TS/Python/PHP/C# parsers, which gate on "is a string" alone and do not
// drop blank strings.
//
// The result is always a non-nil (possibly empty) slice — same always-present
// empty-collection contract as productsByFamilyFromWire, matching the
// TS/Python/PHP parsers. An absent, null, or explicitly-empty field all yield
// an empty slice, never nil.
func stateDerivativesFromWire(raw json.RawMessage) []string {
	elements := decodeRawArray(raw)
	out := make([]string, 0, len(elements))
	for _, el := range elements {
		if isJSONNull(el) {
			// json.Unmarshal of `null` into a string succeeds and yields
			// "", but a null element is not a string — the other parsers
			// reject it. Skip it explicitly so only genuine strings survive.
			continue
		}
		var s string
		if err := json.Unmarshal(el, &s); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// chooseVersion picks the canonical bundle version. The new wire shape
// publishes `catalog_version`; legacy fixtures still emit `version` at
// the data root. Either is accepted.
func chooseVersion(catalog, legacy string) string {
	if catalog != "" {
		return catalog
	}
	return legacy
}

func datasetsV3EntitiesFromWire(rows []datasetsV3WireRow) []ReferenceEntity {
	if len(rows) == 0 {
		return nil
	}
	out := make([]ReferenceEntity, len(rows))
	for i, r := range rows {
		out[i] = ReferenceEntity{ID: r.ID, Name: r.Name}
	}
	return out
}

func relationsFromConditionRows(rows []datasetsV3WireRow) []RelationEdge {
	var edges []RelationEdge
	for _, row := range rows {
		for _, rel := range row.TreatedWith {
			edges = append(edges, RelationEdge{
				FromID:            row.ID,
				ToID:              rel.ID,
				ToName:            rel.Name,
				PrescriptionCount: rel.PrescriptionCount,
			})
		}
	}
	return edges
}

func relationsFromMedicationRows(rows []datasetsV3WireRow) []RelationEdge {
	var edges []RelationEdge
	for _, row := range rows {
		for _, rel := range row.UsedFor {
			edges = append(edges, RelationEdge{
				FromID:            row.ID,
				ToID:              rel.ID,
				ToName:            rel.Name,
				PrescriptionCount: rel.PrescriptionCount,
			})
		}
	}
	return edges
}

func nicotineOptionsFromRows(rows []datasetsV3WireRow) []NicotineOption {
	if len(rows) == 0 {
		return nil
	}
	out := make([]NicotineOption, len(rows))
	for i, r := range rows {
		out[i] = NicotineOption{ID: r.ID, Name: r.Name, Type: r.Type}
	}
	return out
}

func spellingCorrectionsFromRows(rows []datasetsV3WireRow) []SpellingCorrection {
	if len(rows) == 0 {
		return nil
	}
	out := make([]SpellingCorrection, len(rows))
	for i, r := range rows {
		out[i] = SpellingCorrection{ID: r.ID, From: r.From, To: r.To}
	}
	return out
}
