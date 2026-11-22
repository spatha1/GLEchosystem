package model

// ALTB-10 — output files: what a client's ledger (or any downstream system) imports, produced
// from posted journal lines. Configured, not coded: an output layout lists the fields of a record
// and where each value comes from; an output profile chooses the lines (ledger, accounts, journal
// types, dates), how they become records (one per line, summed, or netted, with an optional daily
// offset record), the file format (fixed width, delimited or JSON lines), the order and the file
// name. A fixed-width journal transfer file and a CSV extract are two profiles of the same engine.

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"gl/formula"
)

// OutputLayout is the record layout of output files.
type OutputLayout struct {
	Code         string        `json:"code"`
	Description  string        `json:"description,omitempty"`
	RecordLength int           `json:"recordLength,omitempty"` // fixed width: every record is this long (spaces elsewhere)
	Fields       []OutputField `json:"fields"`
}

// OutputField is one field of a record.
type OutputField struct {
	Name     string   `json:"name"`
	Start    int      `json:"start,omitempty"`  // fixed width: first position (1-based)
	Length   int      `json:"length,omitempty"` // fixed width: width; delimited: maximum length (0: any)
	Source   string   `json:"source"`           // see OutputSources
	Value    string   `json:"value,omitempty"`  // CONSTANT: the value; other sources: the value when empty
	Refs     []string `json:"refs,omitempty"`   // DIMENSION: the dimension; KEY: the keys, first found
	Format   string   `json:"format,omitempty"` // DATE: e.g. yyyyMMdd; PERIOD: yyyyppp or yyyy-MM; AMOUNT: IMPLIED or DECIMAL
	Decimals int      `json:"decimals,omitempty"`
	Align    string   `json:"align,omitempty"`   // LEFT (default) | RIGHT
	Pad      string   `json:"pad,omitempty"`     // fixed width: padding character (default space)
	Indent   int      `json:"indent,omitempty"`  // spaces before the value
	Lengths  []int    `json:"lengths,omitempty"` // the value is written only when its length is one of these
}

// OutputSources are where a field's value can come from.
var OutputSources = []string{"CONSTANT", "ACCOUNT", "CLASS", "ENTITY", "CURRENCY", "DATE", "PERIOD", "AMOUNT", "SIGNED_AMOUNT", "SIDE", "JOURNAL_TYPE", "DIMENSION", "KEY"}

// OutputProfile is one output file.
type OutputProfile struct {
	Code           string          `json:"code"`
	Description    string          `json:"description,omitempty"`
	Format         string          `json:"format"` // FIXED_WIDTH | DELIMITED | JSON_LINES
	Layout         string          `json:"layout"`
	FileName       string          `json:"fileName,omitempty"` // e.g. GL-{TO:yyMMdd}.{PROFILE}.trf
	Entities       []string        `json:"entities,omitempty"` // legal entities (empty: all)
	Delimiter      string          `json:"delimiter,omitempty"`
	Header         bool            `json:"header,omitempty"`
	Sort           []string        `json:"sort,omitempty"`      // field names, in order
	Reversals      string          `json:"reversals,omitempty"` // EXCLUDE (default: the ledger reverses itself) | INCLUDE
	FieldOverrides []OutputField   `json:"fieldOverrides,omitempty"`
	Sections       []OutputSection `json:"sections"`
}

// OutputSection chooses lines and how they become records.
type OutputSection struct {
	Accounts        []string      `json:"accounts,omitempty"`
	ExcludeAccounts []string      `json:"excludeAccounts,omitempty"`
	JournalTypes    []string      `json:"journalTypes,omitempty"`
	Group           string        `json:"group"`                 // LINE | SUMMARY (summed by groupBy and side) | NET (netted by groupBy, side from the sign)
	GroupBy         []string      `json:"groupBy,omitempty"`     // ACCOUNT, DATE, PERIOD, ENTITY, CURRENCY, JOURNAL_TYPE, DIM:<code>, KEY:<code>
	DailyOffset     string        `json:"dailyOffset,omitempty"` // "" | SAME_DAY | MONTH_END: one record per account and day, the day's net, opposite side
	OffsetOverrides []OutputField `json:"offsetOverrides,omitempty"`
}

// OutputLine is one posted line as an output file reads it.
type OutputLine struct {
	Account, Class, Entity, Currency, Side, JournalType string
	Amount                                              *big.Rat
	BookDate                                            time.Time
	Dimensions, Keys                                    map[string]string
	Reversal                                            bool
}

// OutputFile is a produced file and its checks.
type OutputFile struct {
	Profile  string   `json:"profile"`
	Name     string   `json:"name"`
	Format   string   `json:"format"`
	Records  int      `json:"records"`
	Debit    string   `json:"debit"`
	Credit   string   `json:"credit"`
	Balanced bool     `json:"balanced"`
	Checks   []string `json:"checks"`
	Content  string   `json:"content"`
}

type outRecord struct {
	vals      map[string]string // by source key: ACCOUNT, DATE (yyyy-MM-dd), SIDE, DIM:x, KEY:x, …
	amount    *big.Rat          // magnitude
	side      string
	date      time.Time
	overrides []OutputField // a daily offset record's own field values
}

// fiscalPeriod is the year (in which the financial year ends) and period of a date, with the
// default date policy's first month (July: 2026-07 is 2027/001).
func fiscalPeriod(d *Document, t time.Time) (int, int) {
	start := 1
	for _, p := range d.DatePolicies {
		if len(p.EventTypes) == 0 && p.FiscalYearStartMonth > 0 {
			start = p.FiscalYearStartMonth
		}
	}
	m := int(t.Month())
	y := t.Year()
	if start == 1 {
		return y, m
	}
	if m >= start {
		return y + 1, m - start + 1
	}
	return y, m + 12 - start + 1
}

func formatDate(t time.Time, f string) string {
	if f == "" {
		f = "yyyy-MM-dd"
	}
	r := strings.NewReplacer("yyyy", t.Format("2006"), "yy", t.Format("06"), "MM", t.Format("01"), "dd", t.Format("02"), "HH", t.Format("15"), "mm", t.Format("04"), "ss", t.Format("05"))
	return r.Replace(f)
}

func lineKey(by string, l OutputLine, d *Document) string {
	switch {
	case by == "ACCOUNT":
		return l.Account
	case by == "CLASS":
		return l.Class
	case by == "ENTITY":
		return l.Entity
	case by == "CURRENCY":
		return l.Currency
	case by == "DATE":
		return l.BookDate.Format("2006-01-02")
	case by == "PERIOD":
		y, p := fiscalPeriod(d, l.BookDate)
		return fmt.Sprintf("%d/%d", y, p)
	case by == "JOURNAL_TYPE":
		return l.JournalType
	case by == "SIDE":
		return l.Side
	case strings.HasPrefix(by, "DIM:"):
		return l.Dimensions[by[4:]]
	case strings.HasPrefix(by, "KEY:"):
		return l.Keys[by[4:]]
	}
	return ""
}

// values of a record: the group-by values; for a single line (or when every line agrees) also
// its account, entity, currency, journal type; dimensions and keys only when grouped by them (a
// summed record's other analysis is blank) or for a line record.
func recordValues(d *Document, lines []OutputLine, groupBy []string, single bool) map[string]string {
	v := map[string]string{}
	first := lines[0]
	for _, k := range []string{"ACCOUNT", "CLASS", "ENTITY", "CURRENCY", "JOURNAL_TYPE", "DATE", "PERIOD"} {
		val := lineKey(k, first, d)
		same := true
		for _, l := range lines[1:] {
			if lineKey(k, l, d) != val {
				same = false
			}
		}
		if same {
			v[k] = val
		}
	}
	if single {
		for k, x := range first.Dimensions {
			v["DIM:"+k] = x
		}
		for k, x := range first.Keys {
			v["KEY:"+k] = x
		}
	}
	for _, g := range groupBy {
		v[g] = lineKey(g, first, d)
	}
	return v
}

func signed(l OutputLine) *big.Rat {
	r := new(big.Rat).Set(l.Amount)
	if l.Side == "C" {
		r.Neg(r)
	}
	return r
}

// ProduceFile makes the output file of a profile from posted lines booked in [from, to].
func ProduceFile(d *Document, code string, lines []OutputLine, from, to time.Time) (*OutputFile, error) {
	var prof *OutputProfile
	for i := range d.OutputProfiles {
		if d.OutputProfiles[i].Code == code {
			prof = &d.OutputProfiles[i]
		}
	}
	if prof == nil {
		return nil, &CodeError{"UNKNOWN_PROFILE", "output profile " + code + " is not in this configuration"}
	}
	var layout *OutputLayout
	for i := range d.OutputLayouts {
		if d.OutputLayouts[i].Code == prof.Layout {
			layout = &d.OutputLayouts[i]
		}
	}
	if layout == nil {
		return nil, &CodeError{"UNKNOWN_LAYOUT", "output layout " + prof.Layout + " is not in this configuration"}
	}
	fields := effectiveFields(layout.Fields, prof.FieldOverrides)
	in := func(xs []string, x string) bool {
		for _, y := range xs {
			if y == x {
				return true
			}
		}
		return false
	}
	var recs []outRecord
	for _, sec := range prof.Sections {
		var sel []OutputLine
		for _, l := range lines {
			if l.BookDate.Before(from) || l.BookDate.After(to) || (l.Reversal && prof.Reversals != "INCLUDE") ||
				(len(prof.Entities) > 0 && !in(prof.Entities, l.Entity)) || (len(sec.Accounts) > 0 && !in(sec.Accounts, l.Account)) ||
				in(sec.ExcludeAccounts, l.Account) || (len(sec.JournalTypes) > 0 && !in(sec.JournalTypes, l.JournalType)) {
				continue
			}
			sel = append(sel, l)
		}
		switch sec.Group {
		case "LINE":
			for _, l := range sel {
				recs = append(recs, outRecord{vals: recordValues(d, []OutputLine{l}, nil, true), amount: new(big.Rat).Set(l.Amount), side: l.Side, date: l.BookDate})
			}
		case "SUMMARY", "NET":
			by := sec.GroupBy
			if sec.Group == "SUMMARY" && !in(by, "SIDE") {
				by = append(append([]string{}, by...), "SIDE")
			}
			groups := map[string][]OutputLine{}
			var order []string
			for _, l := range sel {
				parts := make([]string, len(by))
				for i, b := range by {
					parts[i] = lineKey(b, l, d)
				}
				k := strings.Join(parts, "\x00")
				if _, ok := groups[k]; !ok {
					order = append(order, k)
				}
				groups[k] = append(groups[k], l)
			}
			for _, k := range order {
				g := groups[k]
				sum := new(big.Rat)
				for _, l := range g {
					sum.Add(sum, signed(l))
				}
				side := "D"
				if sum.Sign() < 0 {
					side = "C"
				}
				recs = append(recs, outRecord{vals: recordValues(d, g, sec.GroupBy, false), amount: new(big.Rat).Abs(sum), side: side, date: g[0].BookDate})
			}
		default:
			return nil, &CodeError{"INVALID_GROUP", "section group must be LINE, SUMMARY or NET"}
		}
		if sec.DailyOffset != "" {
			type dk struct{ account, day string }
			net := map[dk]*big.Rat{}
			first := map[dk]OutputLine{}
			var days []dk
			for _, l := range sel {
				k := dk{l.Account, l.BookDate.Format("2006-01-02")}
				if _, ok := net[k]; !ok {
					net[k] = new(big.Rat)
					first[k] = l
					days = append(days, k)
				}
				net[k].Add(net[k], signed(l))
			}
			for _, k := range days {
				l := first[k]
				date := l.BookDate
				if sec.DailyOffset == "MONTH_END" {
					date = monthEnd(date)
				}
				side := "C" // the opposite of the day's net
				if net[k].Sign() < 0 {
					side = "D"
				}
				v := recordValues(d, []OutputLine{l}, nil, false)
				v["DATE"] = date.Format("2006-01-02")
				recs = append(recs, outRecord{vals: v, amount: new(big.Rat).Abs(net[k]), side: side, date: date, overrides: sec.OffsetOverrides})
			}
		}
	}
	// Render.
	out := &OutputFile{Profile: prof.Code, Format: prof.Format, Checks: []string{}}
	type rendered struct {
		text string
		rec  outRecord
		sort []string
	}
	var rs []rendered
	for _, rec := range recs {
		fs := effectiveFields(fields, rec.overrides)
		vals := make([]string, len(fs))
		for i, f := range fs {
			vals[i] = fieldValue(d, f, rec)
		}
		var text string
		switch prof.Format {
		case "FIXED_WIDTH":
			buf := []byte(strings.Repeat(" ", layout.RecordLength))
			for i, f := range fs {
				v := fit(vals[i], f)
				copy(buf[f.Start-1:], v)
			}
			text = string(buf)
		case "DELIMITED":
			text = delimited(vals, prof.Delimiter)
		case "JSON_LINES":
			obj := map[string]string{}
			for i, f := range fs {
				obj[f.Name] = vals[i]
			}
			b, _ := json.Marshal(obj)
			text = string(b)
		default:
			return nil, &CodeError{"INVALID_FORMAT", "format must be FIXED_WIDTH, DELIMITED or JSON_LINES"}
		}
		var sk []string
		for _, sname := range prof.Sort {
			for i, f := range fs {
				if f.Name == sname {
					sk = append(sk, vals[i])
				}
			}
		}
		rs = append(rs, rendered{text, rec, sk})
	}
	sort.SliceStable(rs, func(i, j int) bool {
		for k := range rs[i].sort {
			if rs[i].sort[k] != rs[j].sort[k] {
				return rs[i].sort[k] < rs[j].sort[k]
			}
		}
		return false
	})
	var b strings.Builder
	if prof.Format == "DELIMITED" && prof.Header {
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Name
		}
		b.WriteString(delimited(names, prof.Delimiter) + "\n")
	}
	dr, cr := new(big.Rat), new(big.Rat)
	byDay := map[string]*big.Rat{}
	for _, r := range rs {
		b.WriteString(r.text + "\n")
		day := r.rec.date.Format("2006-01-02")
		if byDay[day] == nil {
			byDay[day] = new(big.Rat)
		}
		if r.rec.side == "D" {
			dr.Add(dr, r.rec.amount)
			byDay[day].Add(byDay[day], r.rec.amount)
		} else {
			cr.Add(cr, r.rec.amount)
			byDay[day].Sub(byDay[day], r.rec.amount)
		}
		if prof.Format == "FIXED_WIDTH" && len(r.text) != layout.RecordLength {
			out.Checks = append(out.Checks, fmt.Sprintf("a record is %d characters, not %d", len(r.text), layout.RecordLength))
		}
	}
	out.Content, out.Records = b.String(), len(rs)
	out.Debit, out.Credit = dr.FloatString(2), cr.FloatString(2)
	out.Balanced = dr.Cmp(cr) == 0
	if !out.Balanced {
		out.Checks = append(out.Checks, fmt.Sprintf("debits %s ≠ credits %s", out.Debit, out.Credit))
	}
	if prof.Format == "FIXED_WIDTH" && len(out.Checks) == 0 {
		out.Checks = append(out.Checks, fmt.Sprintf("every record is %d characters", layout.RecordLength))
	}
	out.Name = fileName(prof, from, to)
	return out, nil
}

func effectiveFields(fields []OutputField, overrides []OutputField) []OutputField {
	out := append([]OutputField{}, fields...)
	for _, o := range overrides {
		for i, f := range out {
			if f.Name == o.Name {
				if o.Start == 0 {
					o.Start = f.Start
				}
				if o.Length == 0 {
					o.Length = f.Length
				}
				out[i] = o
			}
		}
	}
	return out
}

func fieldValue(d *Document, f OutputField, r outRecord) string {
	v := ""
	switch f.Source {
	case "CONSTANT":
		return f.Value
	case "ACCOUNT", "CLASS", "ENTITY", "CURRENCY", "JOURNAL_TYPE":
		v = r.vals[f.Source]
	case "SIDE":
		v = r.side
	case "DATE":
		if t, ok := ParseDay(r.vals["DATE"]); ok {
			v = formatDate(t, f.Format)
		} else {
			v = formatDate(r.date, f.Format)
		}
	case "PERIOD":
		t := r.date
		if x, ok := ParseDay(r.vals["DATE"]); ok {
			t = x
		}
		y, p := fiscalPeriod(d, t)
		switch f.Format {
		case "yyyy-MM":
			v = t.Format("2006-01")
		default:
			v = fmt.Sprintf("%d%03d", y, p)
		}
	case "AMOUNT", "SIGNED_AMOUNT":
		a := new(big.Rat).Set(r.amount)
		if f.Source == "SIGNED_AMOUNT" && r.side == "C" {
			a.Neg(a)
		}
		s := a.FloatString(f.Decimals)
		if f.Format == "IMPLIED" {
			s = strings.Replace(strings.TrimPrefix(s, "-"), ".", "", 1)
		}
		v = s
	case "DIMENSION":
		if len(f.Refs) > 0 {
			v = r.vals["DIM:"+f.Refs[0]]
		}
	case "KEY":
		for _, k := range f.Refs {
			if x := r.vals["KEY:"+k]; x != "" {
				v = x
				break
			}
		}
	}
	if len(f.Lengths) > 0 {
		ok := false
		for _, n := range f.Lengths {
			ok = ok || len(v) == n
		}
		if !ok {
			v = ""
		}
	}
	if v == "" && f.Source != "CONSTANT" {
		v = f.Value
	}
	if v != "" && f.Indent > 0 {
		v = strings.Repeat(" ", f.Indent) + v
	}
	return v
}

// fit pads or cuts a value to a fixed-width field.
func fit(v string, f OutputField) string {
	pad := " "
	if f.Pad != "" {
		pad = f.Pad[:1]
	}
	if len(v) > f.Length {
		if f.Align == "RIGHT" {
			return v[len(v)-f.Length:]
		}
		return v[:f.Length]
	}
	fill := strings.Repeat(pad, f.Length-len(v))
	if f.Align == "RIGHT" {
		return fill + v
	}
	return v + fill
}

func delimited(vals []string, delim string) string {
	if delim == "" {
		delim = ","
	}
	out := make([]string, len(vals))
	for i, v := range vals {
		if strings.ContainsAny(v, delim+"\"\n") {
			v = `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
		}
		out[i] = v
	}
	return strings.Join(out, delim)
}

func fileName(p *OutputProfile, from, to time.Time) string {
	n := p.FileName
	if n == "" {
		n = "{PROFILE}-{FROM:yyyyMMdd}-{TO:yyyyMMdd}." + map[string]string{"FIXED_WIDTH": "txt", "DELIMITED": "csv", "JSON_LINES": "jsonl"}[p.Format]
	}
	var b strings.Builder
	for {
		i := strings.Index(n, "{")
		if i < 0 {
			b.WriteString(n)
			break
		}
		j := strings.Index(n[i:], "}")
		if j < 0 {
			b.WriteString(n)
			break
		}
		b.WriteString(n[:i])
		tok := n[i+1 : i+j]
		name, f, _ := strings.Cut(tok, ":")
		switch name {
		case "PROFILE":
			b.WriteString(p.Code)
		case "FROM":
			b.WriteString(formatDate(from, f))
		case "TO":
			b.WriteString(formatDate(to, f))
		default:
			b.WriteString("{" + tok + "}")
		}
		n = n[i+j+1:]
	}
	return b.String()
}

// OutputLinesOf turns a store's posted lines and their journals into output lines.
func OutputLinesOf(d *Document, lines []LedgerLine, journals []JournalEntry) ([]OutputLine, error) {
	type info struct {
		dims map[string]string
		jt   string
	}
	byLine := map[string]info{}
	for _, j := range journals {
		for _, l := range j.Lines {
			byLine[l.LineID] = info{l.Dimensions, j.JournalType}
		}
	}
	out := make([]OutputLine, 0, len(lines))
	for _, l := range lines {
		day, ok := ParseDay(l.BookDate)
		if !ok {
			return nil, &CodeError{"MISSING_BOOK_DATE", "a posted line has no book date: " + l.LineID}
		}
		a, err := formula.ParseDecimal(l.Amount)
		if err != nil {
			return nil, err
		}
		in := byLine[l.LineID]
		out = append(out, OutputLine{Account: l.Account, Class: l.Class, Entity: l.Entity, Currency: l.Currency, Side: l.Side, JournalType: in.jt,
			Amount: a, BookDate: day, Dimensions: in.dims, Keys: l.Keys, Reversal: l.Reversal})
	}
	return out, nil
}

func init() { validators = append(validators, validateOutputs) }

func validateOutputs(d *Document) []Finding {
	var fs []Finding
	src := map[string]bool{}
	for _, s := range OutputSources {
		src[s] = true
	}
	dims := map[string]bool{}
	for _, x := range d.Dimensions {
		dims[x.Code] = true
	}
	layouts := map[string]OutputLayout{}
	for i, l := range d.OutputLayouts {
		p := fmt.Sprintf("outputLayouts/%d", i)
		if !isCode(l.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "layout code %q must be an upper-case code", l.Code))
		}
		if _, dup := layouts[l.Code]; dup {
			fs = append(fs, errf(p, "DUPLICATE_LAYOUT", "layout %s is declared twice", l.Code))
		}
		layouts[l.Code] = l
		used := make([]string, l.RecordLength+1)
		names := map[string]bool{}
		for j, f := range l.Fields {
			fp := fmt.Sprintf("%s/fields/%d", p, j)
			if f.Name == "" || names[f.Name] {
				fs = append(fs, errf(fp, "INVALID_FIELD", "field %d needs a unique name", j+1))
			}
			names[f.Name] = true
			if !src[f.Source] {
				fs = append(fs, errf(fp, "UNKNOWN_SOURCE", "field %s: source %q is not one of %s", f.Name, f.Source, strings.Join(OutputSources, ", ")))
			}
			if f.Source == "DIMENSION" && (len(f.Refs) != 1 || !dims[f.Refs[0]]) {
				fs = append(fs, errf(fp, "UNKNOWN_DIMENSION", "field %s reads dimension %v, which is not configured", f.Name, f.Refs))
			}
			if l.RecordLength > 0 {
				if f.Start < 1 || f.Length < 1 || f.Start+f.Length-1 > l.RecordLength {
					fs = append(fs, errf(fp, "OUTSIDE_RECORD", "field %s (%d, %d) is not inside the %d-character record", f.Name, f.Start, f.Length, l.RecordLength))
					continue
				}
				for k := f.Start; k < f.Start+f.Length; k++ {
					if used[k] != "" {
						fs = append(fs, errf(fp, "OVERLAPPING_FIELDS", "field %s overlaps field %s at position %d", f.Name, used[k], k))
						break
					}
					used[k] = f.Name
				}
			}
		}
	}
	accounts := map[string]bool{}
	for _, a := range d.Accounts {
		accounts[a.Code] = true
	}
	seen := map[string]bool{}
	for i, p := range d.OutputProfiles {
		pp := fmt.Sprintf("outputProfiles/%d", i)
		if !isCode(p.Code) || seen[p.Code] {
			fs = append(fs, errf(pp, "INVALID_CODE", "output profile code %q must be a unique upper-case code", p.Code))
		}
		seen[p.Code] = true
		l, ok := layouts[p.Layout]
		if !ok {
			fs = append(fs, errf(pp, "UNKNOWN_LAYOUT", "profile %s uses layout %q, which is not configured", p.Code, p.Layout))
		}
		switch p.Format {
		case "FIXED_WIDTH":
			if ok && l.RecordLength < 1 {
				fs = append(fs, errf(pp, "INVALID_FORMAT", "profile %s is fixed width but layout %s has no record length", p.Code, p.Layout))
			}
		case "DELIMITED", "JSON_LINES":
		default:
			fs = append(fs, errf(pp, "INVALID_FORMAT", "profile %s: format must be FIXED_WIDTH, DELIMITED or JSON_LINES", p.Code))
		}
		if len(p.Sections) == 0 {
			fs = append(fs, errf(pp, "NO_SECTIONS", "profile %s needs at least one section choosing lines", p.Code))
		}
		names := map[string]bool{}
		for _, f := range l.Fields {
			names[f.Name] = true
		}
		check := func(path string, fsx []OutputField) {
			for _, o := range fsx {
				if !names[o.Name] {
					fs = append(fs, errf(path, "UNKNOWN_FIELD", "override of %q: the layout has no such field", o.Name))
				}
				if !src[o.Source] {
					fs = append(fs, errf(path, "UNKNOWN_SOURCE", "override of %s: source %q is not known", o.Name, o.Source))
				}
			}
		}
		check(pp+"/fieldOverrides", p.FieldOverrides)
		for _, s := range p.Sort {
			if !names[s] {
				fs = append(fs, errf(pp+"/sort", "UNKNOWN_FIELD", "sort by %q: the layout has no such field", s))
			}
		}
		for j, s := range p.Sections {
			sp := fmt.Sprintf("%s/sections/%d", pp, j)
			if s.Group != "LINE" && s.Group != "SUMMARY" && s.Group != "NET" {
				fs = append(fs, errf(sp, "INVALID_GROUP", "group must be LINE, SUMMARY or NET"))
			}
			if s.DailyOffset != "" && s.DailyOffset != "SAME_DAY" && s.DailyOffset != "MONTH_END" {
				fs = append(fs, errf(sp, "INVALID_OFFSET", "daily offset must be SAME_DAY or MONTH_END"))
			}
			for _, a := range append(append([]string{}, s.Accounts...), s.ExcludeAccounts...) {
				if !accounts[a] {
					fs = append(fs, errf(sp, "UNKNOWN_ACCOUNT", "account %s is not in the chart of accounts", a))
				}
			}
			for _, g := range s.GroupBy {
				known := g == "ACCOUNT" || g == "CLASS" || g == "ENTITY" || g == "CURRENCY" || g == "DATE" || g == "PERIOD" || g == "JOURNAL_TYPE" || g == "SIDE" ||
					(strings.HasPrefix(g, "DIM:") && dims[g[4:]]) || strings.HasPrefix(g, "KEY:")
				if !known {
					fs = append(fs, errf(sp, "INVALID_GROUP_BY", "group by %q is not known", g))
				}
			}
			check(sp+"/offsetOverrides", s.OffsetOverrides)
		}
	}
	return fs
}
