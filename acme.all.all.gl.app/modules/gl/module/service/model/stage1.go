package model

// Stage 1 (chart of accounts) for clients onboarded from a source system's accounting
// classes, such as a policy administration system's class codes:
//
//   - the standard accounting class library: classes the GL Ecosystem defines, one accounting
//     meaning each, global and read-only like the event catalog. A client's class adopts a
//     library class (standard) or is a client-specific one (custom). A configuration that
//     adopts a class the product owner has not APPROVED does not validate, so nothing is
//     finalised on an unapproved definition;
//   - source classes: the source system's own class codes, kept exactly as received, each with
//     explicit mappings to the client's classes (optionally only when a condition holds, for a
//     source class that means several things);
//   - source files: what was imported, sheet by sheet, so coverage can be checked row by row;
//   - lineage: an account, a source class or an assignment rule can name the file and row it
//     came from, with the original values.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed std_classes.json
var stdClassesJSON []byte

// StandardClass is one class of the library.
type StandardClass struct {
	Code       string `json:"code"`
	Group      string `json:"group"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Nature     string `json:"nature"`
	NormalSide string `json:"normalSide"`
	Status     string `json:"status"` // PROPOSED | APPROVED | RETIRED
}

// ClassLibrary is the standard accounting class library.
type ClassLibrary struct {
	Catalog string          `json:"catalog"`
	Version string          `json:"version"`
	Status  string          `json:"status"`
	Note    string          `json:"note"`
	Classes []StandardClass `json:"classes"`
}

var classLibrary = func() ClassLibrary {
	var l ClassLibrary
	if err := json.Unmarshal(stdClassesJSON, &l); err != nil {
		panic("std_classes.json: " + err.Error())
	}
	return l
}()

// StandardClasses returns the library (a copy).
func StandardClasses() ClassLibrary {
	l := classLibrary
	l.Classes = append([]StandardClass(nil), classLibrary.Classes...)
	return l
}

// StandardClassOf finds a library class.
func StandardClassOf(code string) (StandardClass, bool) {
	for _, c := range classLibrary.Classes {
		if c.Code == code {
			return c, true
		}
	}
	return StandardClass{}, false
}

// SourceRef is where an item came from: an imported file's sheet and row (1 = the first data
// row), with the row's original values, or a passage of a document.
type SourceRef struct {
	File   string            `json:"file,omitempty"` // a sourceFiles id
	Sheet  string            `json:"sheet,omitempty"`
	Row    int               `json:"row,omitempty"`
	Values map[string]string `json:"values,omitempty"` // the original cells, by column header
	Text   string            `json:"text,omitempty"`   // a quoted passage, when not a table row
}

// SourceClassMapping maps a source class to a client class, optionally only when a condition
// (dimension → value, * any present value) holds.
type SourceClassMapping struct {
	Class string            `json:"class"`
	When  map[string]string `json:"when,omitempty"`
	Note  string            `json:"note,omitempty"`
}

// SourceClass is a source system's accounting class code, kept as received.
type SourceClass struct {
	System   string               `json:"system"` // e.g. the policy administration system's code
	Code     string               `json:"code"`   // exactly as in the source, never renamed
	Meaning  string               `json:"meaning,omitempty"`
	Role     string               `json:"role,omitempty"`
	Status   string               `json:"status,omitempty"` // UNMAPPED | MAPPED | NOT_USED; default from the mappings
	Mappings []SourceClassMapping `json:"mappings,omitempty"`
	Note     string               `json:"note,omitempty"`
	Source   *SourceRef           `json:"source,omitempty"`
}

// SkippedRow is a source row an import did not use, and why.
type SkippedRow struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// SourceFile records one imported sheet.
type SourceFile struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Sheet      string            `json:"sheet,omitempty"`
	Kind       string            `json:"kind"`              // COA | CLASSES | ASSIGNMENTS | OTHER
	Columns    map[string]string `json:"columns,omitempty"` // role → column header
	Conditions []string          `json:"conditions,omitempty"`
	Rows       int               `json:"rows"`
	Skipped    []SkippedRow      `json:"skipped,omitempty"`
	Digest     string            `json:"digest,omitempty"`
	ImportedBy string            `json:"importedBy,omitempty"`
	ImportedAt string            `json:"importedAt,omitempty"`
}

var (
	sourceFileKinds   = []string{"COA", "CLASSES", "ASSIGNMENTS", "OTHER"}
	sourceClassStatus = []string{"UNMAPPED", "MAPPED", "NOT_USED"}
)

// MappingStatus is a source class's status: as declared, else MAPPED when it has mappings.
func (s SourceClass) MappingStatus() string {
	if s.Status != "" {
		return s.Status
	}
	if len(s.Mappings) > 0 {
		return "MAPPED"
	}
	return "UNMAPPED"
}

// serves says whether an account may receive lines of a class: the class (or classes) it
// declares; an account that declares none serves the classes its assignment rules give it.
func (d *Document) serves(a Account, class string) bool {
	if a.Class != "" || len(a.Classes) > 0 {
		return a.Class == class || oneOf(class, a.Classes)
	}
	for _, r := range d.AssignmentRules {
		if r.Account == a.Code && r.Class == class && r.ledger() == a.LedgerOf() {
			return true
		}
	}
	return false
}

// declaredClasses are the classes an account names itself.
func (a Account) declaredClasses() []string {
	out := append([]string{}, a.Classes...)
	if a.Class != "" && !oneOf(a.Class, out) {
		out = append([]string{a.Class}, out...)
	}
	return out
}

func init() { validators = append(validators, validateStage1) }

func validateStage1(d *Document) []Finding {
	var fs []Finding
	classes := map[string]AccountingClass{}
	for _, c := range d.AccountingClasses {
		classes[c.Code] = c
	}
	// classes adopted from the library
	for i, c := range d.AccountingClasses {
		p := fmt.Sprintf("accountingClasses/%d", i)
		if c.Standard == "" {
			continue
		}
		if c.Custom {
			fs = append(fs, errf(p, "INVALID_CLASS", "class %s cannot be both custom and the standard class %s", c.Code, c.Standard))
			continue
		}
		std, ok := StandardClassOf(c.Standard)
		switch {
		case !ok:
			fs = append(fs, errf(p, "UNKNOWN_STANDARD_CLASS", "class %s adopts %s, which is not in the standard class library", c.Code, c.Standard))
		case std.Status == "RETIRED":
			fs = append(fs, errf(p, "STANDARD_CLASS_RETIRED", "class %s adopts %s, which the library has retired", c.Code, c.Standard))
		case std.Status != "APPROVED":
			fs = append(fs, errf(p, "STANDARD_CLASS_NOT_APPROVED", "class %s adopts the standard class %s, which is %s in library %s: it can be drafted, but not validated until the class is approved", c.Code, c.Standard, strings.ToLower(std.Status), classLibrary.Version))
		}
		if ok && c.Nature != "" && c.Nature != std.Nature {
			fs = append(fs, errf(p, "STANDARD_CLASS_DIFFERS", "class %s is %s, but the standard class %s is %s", c.Code, c.Nature, c.Standard, std.Nature))
		}
	}
	// accounts: declared classes, type and normal balance
	for i, a := range d.Accounts {
		p := fmt.Sprintf("accounts/%d", i)
		for _, c := range a.Classes {
			if _, ok := classes[c]; !ok {
				fs = append(fs, errf(p, "UNKNOWN_CLASS", "account %s serves class %q, which is not an accounting class", a.Code, c))
			}
		}
		if a.Type != "" && !oneOf(a.Type, natures) {
			fs = append(fs, errf(p, "INVALID_NATURE", "account %s type %q must be one of %v", a.Code, a.Type, natures))
		}
		if a.NormalBalance != "" && a.NormalBalance != "D" && a.NormalBalance != "C" {
			fs = append(fs, errf(p, "INVALID_SIDE", "account %s normal balance %q must be D or C", a.Code, a.NormalBalance))
		}
		fs = append(fs, checkSourceRef(d, p, a.Source)...)
	}
	for i, r := range d.AssignmentRules {
		fs = append(fs, checkSourceRef(d, fmt.Sprintf("accountAssignmentRules/%d", i), r.Source)...)
	}
	// source classes and their mappings
	seen := map[string]bool{}
	for i, s := range d.SourceClasses {
		p := fmt.Sprintf("sourceClasses/%d", i)
		if strings.TrimSpace(s.System) == "" || strings.TrimSpace(s.Code) == "" {
			fs = append(fs, errf(p, "MISSING_CODE", "a source class needs its system and its code"))
			continue
		}
		id := s.System + "/" + s.Code
		if seen[id] {
			fs = append(fs, errf(p, "DUPLICATE_SOURCE_CLASS", "source class %s of %s is listed twice", s.Code, s.System))
		}
		seen[id] = true
		if s.Status != "" && !oneOf(s.Status, sourceClassStatus) {
			fs = append(fs, errf(p, "INVALID_STATUS", "source class %s status %q must be one of %v", s.Code, s.Status, sourceClassStatus))
		}
		conditional := 0
		for j, m := range s.Mappings {
			mp := fmt.Sprintf("%s/mappings/%d", p, j)
			if _, ok := classes[m.Class]; !ok {
				fs = append(fs, errf(mp, "UNKNOWN_CLASS", "source class %s maps to %q, which is not an accounting class of this configuration", s.Code, m.Class))
			}
			if len(m.When) > 0 {
				conditional++
			}
		}
		if len(s.Mappings) > 1 && conditional < len(s.Mappings)-1 {
			fs = append(fs, errf(p, "AMBIGUOUS_SOURCE_MAPPING", "source class %s maps to %d classes: all but one need a condition saying when each applies", s.Code, len(s.Mappings)))
		}
		switch s.MappingStatus() {
		case "UNMAPPED":
			fs = append(fs, warnf(p, "SOURCE_CLASS_UNMAPPED", "source class %s (%s) is not mapped to a class yet", s.Code, s.System))
		case "MAPPED":
			if len(s.Mappings) == 0 {
				fs = append(fs, errf(p, "SOURCE_CLASS_UNMAPPED", "source class %s is marked mapped but has no mapping", s.Code))
			}
		}
		fs = append(fs, checkSourceRef(d, p, s.Source)...)
	}
	// source files
	files := map[string]bool{}
	for i, f := range d.SourceFiles {
		p := fmt.Sprintf("sourceFiles/%d", i)
		if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Name) == "" {
			fs = append(fs, errf(p, "MISSING_CODE", "a source file needs an id and a name"))
		}
		if files[f.ID] {
			fs = append(fs, errf(p, "DUPLICATE_SOURCE_FILE", "source file id %s is listed twice", f.ID))
		}
		files[f.ID] = true
		if !oneOf(f.Kind, sourceFileKinds) {
			fs = append(fs, errf(p, "INVALID_KIND", "source file %s kind %q must be one of %v", f.Name, f.Kind, sourceFileKinds))
		}
		if f.Rows < 0 {
			fs = append(fs, errf(p, "INVALID_ROWS", "source file %s rows must not be negative", f.Name))
		}
		for _, s := range f.Skipped {
			if s.Row < 1 || s.Row > f.Rows || strings.TrimSpace(s.Reason) == "" {
				fs = append(fs, errf(p, "INVALID_SKIPPED_ROW", "source file %s: a skipped row needs a row number within 1..%d and a reason", f.Name, f.Rows))
				break
			}
		}
	}
	return fs
}

func checkSourceRef(d *Document, p string, s *SourceRef) []Finding {
	if s == nil || s.File == "" {
		return nil
	}
	for _, f := range d.SourceFiles {
		if f.ID == s.File {
			if s.Row < 1 || (f.Rows > 0 && s.Row > f.Rows) {
				return []Finding{errf(p+"/source", "INVALID_SOURCE_ROW", "row %d is not a data row of %s (1..%d)", s.Row, f.Name, f.Rows)}
			}
			return nil
		}
	}
	return []Finding{errf(p+"/source", "UNKNOWN_SOURCE_FILE", "source file %q is not among the imported files", s.File)}
}

// MapSourceClass resolves a source class to the client class for a row's values: the mapping
// whose condition holds (an unconditional mapping is the fallback); empty when none applies.
func MapSourceClass(d *Document, system, code string, vals map[string]string) (string, string) {
	for _, s := range d.SourceClasses {
		if s.System != system || s.Code != code {
			continue
		}
		fallback := ""
		for _, m := range s.Mappings {
			if len(m.When) == 0 {
				fallback = m.Class
				continue
			}
			ok := true
			for k, v := range m.When {
				got := strings.TrimSpace(vals[k])
				if (v == wildcard && got == "") || (v != wildcard && got != v) {
					ok = false
				}
			}
			if ok {
				return m.Class, "mapping of " + code + " when " + describeWhen(m.When)
			}
		}
		if fallback != "" {
			return fallback, "mapping of " + code
		}
		return "", "no mapping of " + code + " applies"
	}
	return "", code + " is not a source class of " + system
}

func describeWhen(w map[string]string) string {
	keys := make([]string, 0, len(w))
	for k := range w {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " = " + w[k]
	}
	return strings.Join(parts, ", ")
}
