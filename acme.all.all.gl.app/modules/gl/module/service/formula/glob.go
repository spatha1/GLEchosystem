package formula

// Glob reports whether s matches pattern, where * matches any run of characters (including
// none) and ? matches exactly one. Matching is exact otherwise (case-sensitive); canonical
// dimension values are normalised before any filter sees them.
func Glob(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	pi, ti := 0, 0
	star, mark := -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// Comparison is a field compared with string literals in an expression (==, !=, IN): the
// model uses it to reject filters on dimension values that do not exist.
type Comparison struct {
	Field  string
	Values []string
	Pos    int
}

// StringComparisons lists every comparison of a field with string literals.
func StringComparisons(n Node) []Comparison {
	var out []Comparison
	var walk func(Node)
	walk = func(n Node) {
		switch x := n.(type) {
		case unary:
			walk(x.x)
		case binary:
			if x.op == "==" || x.op == "!=" {
				if id, ok := x.l.(ident); ok {
					if s, ok := x.r.(strLit); ok {
						out = append(out, Comparison{id.name, []string{s.s}, x.p})
					}
				}
				if id, ok := x.r.(ident); ok {
					if s, ok := x.l.(strLit); ok {
						out = append(out, Comparison{id.name, []string{s.s}, x.p})
					}
				}
			}
			walk(x.l)
			walk(x.r)
		case call:
			if x.name == "IN" && len(x.args) >= 2 {
				if id, ok := x.args[0].(ident); ok {
					var vals []string
					for _, a := range x.args[1:] {
						if s, ok := a.(strLit); ok {
							vals = append(vals, s.s)
						}
					}
					if len(vals) > 0 {
						out = append(out, Comparison{id.name, vals, x.p})
					}
				}
			}
			for _, a := range x.args {
				walk(a)
			}
		}
	}
	walk(n)
	return out
}
