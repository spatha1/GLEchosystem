package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"gl/logic"
)

// The descriptor must publish the embedded OpenAPI document with every path already
// prefixed /m/<token>/ (the host splices fragments verbatim).
func TestDescriptorPublishesPrefixedOpenAPI(t *testing.T) {
	d := descriptor()
	if string(d.OpenAPI) != logic.OpenAPIFragment {
		t.Fatal("Descriptor.OpenAPI is not the embedded openapi.json")
	}
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(d.OpenAPI, &doc); err != nil || len(doc.Paths) == 0 {
		t.Fatalf("OpenAPI has no paths (%v)", err)
	}
	for p := range doc.Paths {
		if !strings.HasPrefix(p, "/m/"+d.Token+"/") {
			t.Errorf("path %s is not prefixed /m/%s/", p, d.Token)
		}
	}
	if len(d.Migrations) == 0 || d.Route == nil {
		t.Error("descriptor must ship migrations and a route")
	}
}

// The token in code must match the module's mycelfile (schema, route prefix, element).
func TestTokenMatchesMycelfile(t *testing.T) {
	raw, err := os.ReadFile("../mycelfile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^token\s*=\s*"([a-z0-9_]+)"`).FindStringSubmatch(string(raw))
	if m == nil || m[1] != logic.Token {
		t.Fatalf("mycelfile token %v, code token %q", m, logic.Token)
	}
	if !strings.Contains(string(raw), `element  = "`+logic.Token+`-app"`) {
		t.Errorf("mycelfile element must be %s-app", logic.Token)
	}
}
