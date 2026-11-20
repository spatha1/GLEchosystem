package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The validator's enumerations are the schema's: a change to one without the other fails here.
func TestManifestEnumerationsMatchTheSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "design", "02_canonical_contracts", "schemas", "field-manifest.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Defs struct {
			Field struct {
				Properties struct {
					Kind        struct{ Enum []string } `json:"kind"`
					DataType    struct{ Enum []string } `json:"dataType"`
					Cardinality struct{ Enum []string } `json:"cardinality"`
					KeyRole     struct{ Enum []string } `json:"keyRole"`
					RequiredBy  struct {
						Items struct {
							Properties struct {
								Kind struct{ Enum []string } `json:"kind"`
							} `json:"properties"`
						} `json:"items"`
					} `json:"requiredBy"`
				} `json:"properties"`
			} `json:"field"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	p := s.Defs.Field.Properties
	for name, pair := range map[string][2][]string{
		"kind": {p.Kind.Enum, ManifestKinds}, "dataType": {p.DataType.Enum, ManifestDataTypes}, "cardinality": {p.Cardinality.Enum, ManifestCardinalities},
		"keyRole": {p.KeyRole.Enum, ManifestKeyRoles}, "requiredBy.kind": {p.RequiredBy.Items.Properties.Kind.Enum, ManifestRequirements},
	} {
		if !slices.Equal(pair[0], pair[1]) {
			t.Errorf("%s: schema %v, validator %v", name, pair[0], pair[1])
		}
	}
}

// The engine's GL-TEST-001 manifest (a snapshot the engine's tests pin) satisfies C2, asks
// sources for the inputs of derived values and never for the values themselves.
func TestGLTest001ManifestSatisfiesC2(t *testing.T) {
	var m Manifest
	if err := DecodeStrict(filepath.Join("..", "fixtures", "GL-TEST-001.manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	if p := ValidateManifest(m); len(p) > 0 {
		t.Fatalf("C2 violations: %v", p)
	}
	kinds := map[string]string{}
	for _, e := range m.Events {
		for _, f := range e.Fields {
			kinds[e.EventType+" "+f.ListCode+"."+f.FieldCode] = f.Kind
		}
	}
	for k, want := range map[string]string{
		"PAYMENT_RECEIVED .BANK_ACCOUNT": "SOURCE", "PAYMENT_RECEIVED .BANKING_COMPANY": "DERIVED", "PAYMENT_RECEIVED .PAY_AMT": "SOURCE",
		"PAYMENT_RECEIVED .RECEIPT_NO": "SOURCE", "PAYMENT_RECEIVED ALLOCATION_LIST.ALLOC_AMT": "SOURCE", "PAYMENT_RECEIVED ALLOCATION_LIST.INVOICE_NO": "SOURCE",
		"PAYMENT_RECEIVED .TRANSACTION_DATE": "SOURCE", "PAYMENT_RECEIVED .EFFECTIVE_DATE": "SOURCE",
		"PREM_ADDED .PREMIUM": "SOURCE", "PREM_ADDED .TAX": "SOURCE", "PREM_ADDED .GROSS": "DERIVED", "PREM_ADDED .LOB": "SOURCE",
		"INVOICED INVOICE_LIST.INVOICE_AMT": "SOURCE", "INVOICED INVOICE_LIST.INVOICE_NO": "SOURCE",
	} {
		if kinds[k] != want {
			t.Errorf("%s: %q, want %s", k, kinds[k], want)
		}
	}
	for k, kind := range kinds {
		for _, derived := range []string{".BANKING_COMPANY", ".BOOKING_CLASS", ".OPEN_BALANCE"} {
			if len(k) > len(derived) && k[len(k)-len(derived):] == derived && kind == "SOURCE" {
				t.Errorf("%s is requested from a source", k)
			}
		}
	}
}

// Negative: the validator refuses what C2 forbids.
func TestTheManifestContractRejectsBadFields(t *testing.T) {
	var m Manifest
	if err := DecodeStrict(filepath.Join("..", "fixtures", "GL-TEST-001.manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	f := &m.Events[0].Fields[0]
	for name, bad := range map[string]func(){
		"source with derivedFrom": func() { f.DerivedFrom = []string{"X"} },
		"list without listCode":   func() { f.Cardinality = "LIST" },
		"no reason":               func() { f.RequiredBy = nil },
		"unknown kind":            func() { f.Kind = "GUESSED" },
		"bad digest":              func() { m.ManifestDigest = "sha256:abc" },
	} {
		saved, savedDigest := *f, m.ManifestDigest
		bad()
		if p := ValidateManifest(m); len(p) == 0 {
			t.Errorf("%s was accepted", name)
		}
		*f, m.ManifestDigest = saved, savedDigest
	}
}
