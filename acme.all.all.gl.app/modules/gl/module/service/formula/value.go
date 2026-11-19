// Package formula is GL's bounded, deterministic expression language for derived measures,
// filters and posting amounts (US-007). It never executes code or SQL: an expression is
// parsed into a small typed tree and evaluated over exact rational numbers, with explicit
// errors for every unsafe case (division by zero, currency mismatch, nulls in conditions,
// unknown names, excessive size). Evaluation reads only what the caller's Env provides —
// no clock, no randomness, no I/O — so the same inputs always give the same result.
package formula

import (
	"fmt"
	"math/big"
	"strings"
)

// Kind is the type of a value.
type Kind int

const (
	KNull Kind = iota
	KNumber
	KAmount
	KBool
	KString
	KDate
)

func (k Kind) String() string {
	return [...]string{"NULL", "NUMBER", "AMOUNT", "BOOL", "STRING", "DATE"}[k]
}

// Value is one evaluated value. Numbers and amounts are exact rationals; an amount also has a
// currency. A literal number is "untyped": it adopts the currency of an amount it is combined
// with, as an untyped constant does in Go. A computed plain number never silently becomes money.
type Value struct {
	Kind     Kind
	Num      *big.Rat // KNumber, KAmount
	Currency string   // KAmount
	Untyped  bool     // KNumber literal
	Bool     bool
	Str      string // KString; KDate as YYYY-MM-DD
}

// Null is the null value.
var Null = Value{Kind: KNull}

// Number returns a typed plain number.
func Number(r *big.Rat) Value { return Value{Kind: KNumber, Num: new(big.Rat).Set(r)} }

// Amount returns an amount in a currency.
func Amount(r *big.Rat, currency string) Value {
	return Value{Kind: KAmount, Num: new(big.Rat).Set(r), Currency: currency}
}

// Bool returns a boolean value.
func Bool(b bool) Value { return Value{Kind: KBool, Bool: b} }

// String returns a string value.
func String(s string) Value { return Value{Kind: KString, Str: s} }

// Date returns a date value (YYYY-MM-DD).
func Date(s string) Value { return Value{Kind: KDate, Str: s} }

// ParseDecimal parses a plain decimal string ("-12.50") exactly. Exponents, signs other than a
// leading '-', separators and anything else that is not a plain decimal are refused.
func ParseDecimal(s string) (*big.Rat, error) {
	t := s
	if strings.HasPrefix(t, "-") {
		t = t[1:]
	}
	if t == "" {
		return nil, fmt.Errorf("%q is not a decimal", s)
	}
	dot := false
	for i, c := range t {
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && !dot && i > 0 && i < len(t)-1:
			dot = true
		default:
			return nil, fmt.Errorf("%q is not a decimal", s)
		}
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal", s)
	}
	return r, nil
}

// MustDecimal is ParseDecimal for constants in tests and fixtures.
func MustDecimal(s string) *big.Rat {
	r, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return r
}

// Exact formats r as a terminating decimal with exactly `scale` places, or fails if r is not
// representable at that scale without rounding (rounding is always explicit: ROUND).
func Exact(r *big.Rat, scale int) (string, error) {
	m := new(big.Rat).Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)))
	if !m.IsInt() {
		return "", &Error{Code: "NOT_REPRESENTABLE", Message: fmt.Sprintf("%s is not exact at %d decimal places; ROUND it explicitly", r.RatString(), scale)}
	}
	return r.FloatString(scale), nil
}

// Minimal formats a terminating r with the fewest decimals (e.g. 1100, 0.3, 549.5), or
// RatString form ("1/3") for a non-terminating value. Used in lineage.
func Minimal(r *big.Rat) string {
	for scale := 0; scale <= 18; scale++ {
		if s, err := Exact(r, scale); err == nil {
			return s
		}
	}
	return r.RatString()
}

// String renders a value for lineage and messages.
func (v Value) String() string {
	switch v.Kind {
	case KNull:
		return "null"
	case KNumber:
		return Minimal(v.Num)
	case KAmount:
		return Minimal(v.Num) + " " + v.Currency
	case KBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case KString:
		return "'" + v.Str + "'"
	case KDate:
		return v.Str
	}
	return "?"
}

// Error is an evaluation or check error with a stable code.
type Error struct {
	Code    string // UNKNOWN_IDENTIFIER, DIVIDE_BY_ZERO, CURRENCY_MISMATCH, TYPE_MISMATCH, NULL_CONDITION, …
	Message string
	Pos     int // byte offset in the expression, when known
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func errAt(pos int, code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...), Pos: pos}
}
