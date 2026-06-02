package sdk

import (
	"reflect"
	"testing"

	"github.com/isa-sdk/sdk/zyins"
)

// TestLicenseOptions_HasNoDeviceIDField pins the cross-language audit
// finding that the Go SDK must NOT expose DeviceId on LicenseOptions —
// the device id is derived server-side from the keycode + email
// activation flow, not supplied by the caller. The field's absence
// is load-bearing; if a future refactor adds it, this test fails.
func TestLicenseOptions_HasNoDeviceIDField(t *testing.T) {
	checkNoDeviceIDField(t, LicenseOptions{})
	checkNoDeviceIDField(t, BearerOptions{})
	checkNoDeviceIDField(t, SessionOptions{})
}

func checkNoDeviceIDField(t *testing.T, value any) {
	t.Helper()
	typ := reflect.TypeOf(value)
	for f := range typ.Fields() {
		name := f.Name
		if name == "DeviceId" || name == "DeviceID" {
			t.Errorf("%s leaks a %s field — device id must remain server-derived", typ.Name(), name)
		}
	}
}

func TestLicenseOptions_HasAPIVersionAndCaseStorage(t *testing.T) {
	checkHasField(t, LicenseOptions{}, "APIVersion")
	checkHasField(t, LicenseOptions{}, "CaseStorage")
	checkHasField(t, BearerOptions{}, "APIVersion")
	checkHasField(t, BearerOptions{}, "CaseStorage")
	checkHasField(t, SessionOptions{}, "APIVersion")
	checkHasField(t, SessionOptions{}, "CaseStorage")
}

func checkHasField(t *testing.T, value any, field string) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if _, ok := typ.FieldByName(field); !ok {
		t.Errorf("%s missing %s field", typ.Name(), field)
	}
}

func TestBundledAPIVersions_ReExportedAtRoot(t *testing.T) {
	if BundledAPIVersions["prequalify"] != "v3" {
		t.Errorf("sdk.BundledAPIVersions[prequalify] = %q, want v3", BundledAPIVersions["prequalify"])
	}
	// The re-export must mirror the zyins source-of-truth value-by-
	// value so reads stay consistent if a release bumps a surface.
	for surface, want := range zyins.BundledAPIVersions {
		if BundledAPIVersions[surface] != want {
			t.Errorf("re-export drift: %s = %q, zyins = %q", surface, BundledAPIVersions[surface], want)
		}
	}
}

func TestResolveAPIVersion_ReExport(t *testing.T) {
	if v := ResolveAPIVersion(nil, "branding"); v != "v1" {
		t.Errorf("sdk.ResolveAPIVersion(nil, branding) = %q, want v1", v)
	}
	if v := ResolveAPIVersion(map[string]string{"branding": "v9"}, "branding"); v != "v9" {
		t.Errorf("override not honored: got %q, want v9", v)
	}
}
