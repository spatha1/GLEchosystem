package model

// ALTB-09 — journal types and reversing journals. A posting rule may name a journal type (for
// example GJ, a general journal, or RJ, a reversing journal). A type that reverses in the next
// period gets its reversal posted on the first day of the next period, as its own entry linked to
// the original: a month-end level (such as unearned premium) then stands only until the next
// month-end replaces it. The temporary ledger of a preview and the journals of a posting run both
// carry the reversal, so they stay the same.

import (
	"fmt"
	"time"
)

// JournalType is one journal type of the client's ledger.
type JournalType struct {
	Code        string `json:"code"`
	Description string `json:"description,omitempty"`
	Reverse     string `json:"reverse,omitempty"` // "" (never) | NEXT_PERIOD
}

// ReverseNextPeriod is the only reversal so far: on the first day of the next period.
const ReverseNextPeriod = "NEXT_PERIOD"

// reversesNextPeriod says whether entries of this journal type are reversed in the next period.
func reversesNextPeriod(d *Document, code string) bool {
	if code == "" {
		return false
	}
	for _, j := range d.JournalTypes {
		if j.Code == code {
			return j.Reverse == ReverseNextPeriod
		}
	}
	return false
}

// NextPeriodStart is the first day of the period after the one day falls in (periods are months).
func NextPeriodStart(day time.Time) time.Time {
	y, m, _ := day.Date()
	return time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
}

func init() { validators = append(validators, validateJournalTypes) }

func validateJournalTypes(d *Document) []Finding {
	var fs []Finding
	seen := map[string]bool{}
	for i, j := range d.JournalTypes {
		p := fmt.Sprintf("journalTypes/%d", i)
		if !isCode(j.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "journal type %q must be an upper-case code", j.Code))
		}
		if seen[j.Code] {
			fs = append(fs, errf(p, "DUPLICATE_JOURNAL_TYPE", "journal type %s is declared twice", j.Code))
		}
		seen[j.Code] = true
		if j.Reverse != "" && j.Reverse != ReverseNextPeriod {
			fs = append(fs, errf(p, "INVALID_REVERSAL", "journal type %s: reverse must be empty or %s", j.Code, ReverseNextPeriod))
		}
	}
	for i, r := range d.PostingRules {
		if r.JournalType != "" && !seen[r.JournalType] {
			fs = append(fs, errf(fmt.Sprintf("postingRules/%d", i), "UNKNOWN_JOURNAL_TYPE", "rule %s names journal type %q, which is not in Journal types", r.Code, r.JournalType))
		}
	}
	return fs
}
