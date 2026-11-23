package logic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"gl/model"
)

// SchemaVersion is the configuration contract version (spec C1) this module stores.
const SchemaVersion = "gl.config/v1"

// MaxContentBytes bounds one configuration document.
const MaxContentBytes = 2 << 20

// Sections are the content sections a configuration may contain (spec C1). Each section is
// an array of objects. Section semantics are validated by the validators registered for it
// (sectionValidators) — each story that gives a section meaning registers its validator.
var Sections = []string{
	"sourceSystems", "sourceAuthority", "events", "baseMeasures", "derivedMeasures",
	"dimensions", "lookups", "datePolicies", "calendars", "keyProfiles",
	"accountingClasses", "accounts", "postingRules", "accountAssignmentRules",
	"reconciliationEquations", "outputProfiles", "scenarios", "lifecycleProfiles", "journalTypes", "outputLayouts",
	"sourceClasses", "sourceFiles",
}

// Finding is one validation result (shared with the typed model). Severity "error" blocks
// validation; "warning" does not.
type Finding = model.Finding

// Report is the outcome of validating one configuration version.
type Report struct {
	OK       bool      `json:"ok"`
	Findings []Finding `json:"findings"`
}

// SectionValidator validates one section's items (already known to be objects). The full
// content is passed so a section can check references into other sections.
type SectionValidator func(items []map[string]any, content map[string][]map[string]any) []Finding

// sectionValidators is filled by the stories that define each section's meaning.
var sectionValidators = map[string][]SectionValidator{}

// RegisterSectionValidator adds a validator for a known section. Validators run in
// registration order; all of them run, and every finding is reported.
func RegisterSectionValidator(section string, v SectionValidator) {
	for _, s := range Sections {
		if s == section {
			sectionValidators[section] = append(sectionValidators[section], v)
			return
		}
	}
	panic("RegisterSectionValidator: unknown section " + section)
}

// CanonicalContent parses a configuration content document and returns its canonical JSON
// (object keys sorted, numbers kept exactly as written — never through float64, no
// insignificant whitespace, no HTML escaping) and its digest "sha256:<hex>". It also
// enforces the document's shape: a JSON object whose keys are known sections, each an array
// of objects. A nil or empty input means an empty configuration.
func CanonicalContent(raw []byte) (canonical []byte, digest string, shape map[string][]map[string]any, err error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	if len(raw) > MaxContentBytes {
		return nil, "", nil, fmt.Errorf("content exceeds %d bytes", MaxContentBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, "", nil, fmt.Errorf("content is not valid JSON: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, "", nil, fmt.Errorf("content has trailing data after the JSON document")
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, "", nil, fmt.Errorf("content must be a JSON object of sections")
	}
	known := map[string]bool{}
	for _, s := range Sections {
		known[s] = true
	}
	shape = map[string][]map[string]any{}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !known[k] {
			return nil, "", nil, fmt.Errorf("unknown section %q (known: %s)", k, strings.Join(Sections, ", "))
		}
		arr, ok := obj[k].([]any)
		if !ok {
			return nil, "", nil, fmt.Errorf("section %q must be an array", k)
		}
		items := make([]map[string]any, 0, len(arr))
		for i, it := range arr {
			m, ok := it.(map[string]any)
			if !ok {
				return nil, "", nil, fmt.Errorf("section %q item %d must be an object", k, i)
			}
			items = append(items, m)
		}
		shape[k] = items
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return nil, "", nil, err
	}
	canonical = bytes.TrimRight(buf.Bytes(), "\n")
	sum := sha256.Sum256(canonical)
	return canonical, "sha256:" + hex.EncodeToString(sum[:]), shape, nil
}

// ValidateContent validates a configuration: the typed model's cross-section rules
// (package model) plus any registered section validator. Findings are sorted so a report
// is reproducible byte for byte.
func ValidateContent(canonical []byte, shape map[string][]map[string]any) Report {
	fs := model.ValidateCanonical(canonical)
	for _, s := range Sections {
		for _, v := range sectionValidators[s] {
			fs = append(fs, v(shape[s], shape)...)
		}
	}
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Path != fs[j].Path {
			return fs[i].Path < fs[j].Path
		}
		return fs[i].Code < fs[j].Code
	})
	ok := true
	for _, f := range fs {
		if f.Severity == "error" {
			ok = false
		}
	}
	if fs == nil {
		fs = []Finding{}
	}
	return Report{OK: ok, Findings: fs}
}

// ParseDate accepts only a real calendar date in YYYY-MM-DD form.
func ParseDate(field, s string) (time.Time, *apiError) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Format("2006-01-02") != s {
		return time.Time{}, errInvalid(field, field+" must be a real date in YYYY-MM-DD form")
	}
	return t, nil
}

// checkEffective validates an effective window [from, to): to, when given, must be after from.
func checkEffective(from string, to *string) *apiError {
	f, e := ParseDate("effectiveFrom", from)
	if e != nil {
		return e
	}
	if to != nil {
		t, e := ParseDate("effectiveTo", *to)
		if e != nil {
			return e
		}
		if !t.After(f) {
			return errInvalid("effectiveTo", "effectiveTo must be after effectiveFrom (the window is [from, to))")
		}
	}
	return nil
}
