// Package aggregate reads a metric's aggregation, written as pseudocode
// SQL (TAXONOMY.md D64): one aggregate function over one field or every
// entry, and an optional condition on the source's own fields.
//
//	COUNT(*) WHERE graded_at = 'intake'
//	SUM(weight_kg) WHERE grade IN ('first', 'second') AND weight_kg > 0
//
// It is not SQL: it names no table, no join and no other source, which
// belong to the warehouse model behind the source, set outside
// Cartograph. What it reads it says in words (Reading), lists the names
// it uses (Names) and renders as a semantic layer measure (Measure).
package aggregate

import (
	"fmt"
	"strconv"
	"strings"
)

// Aggregation is what a text reads as.
type Aggregation struct {
	// Func is COUNT, SUM, AVG, MIN or MAX.
	Func string
	// Distinct counts each value of Field once.
	Distinct bool
	// Field is what is aggregated; empty for every entry, COUNT(*).
	Field string
	// Where is the condition on the entries counted; nil for all of them.
	Where *Node
}

// Node is a condition: an AND, an OR or a NOT of others, or a comparison.
type Node struct {
	// Op is "and", "or", "not", or a comparison: = != < <= > >= in
	// between like null notnull. A comparison negated by NOT IN, NOT
	// BETWEEN or NOT LIKE carries Not.
	Op    string
	Not   bool
	Kids  []*Node
	Field string
	// Values are the comparison's values, as written (a string keeps its
	// quotes).
	Values []string
}

// Problem is why a text does not read as an aggregation, and where.
type Problem struct {
	Message string
	// At is the byte offset in the text it is about.
	At int
}

func (p Problem) Error() string { return p.Message }

// Parse reads text as an aggregation. A text that does not read as one
// answers the first problem found.
func Parse(text string) (Aggregation, error) {
	if strings.TrimSpace(text) == "" {
		return Aggregation{}, Problem{Message: "Write the aggregation: an aggregate function over a field, such as SUM(weight_kg), or COUNT(*) to count every entry."}
	}
	toks, err := lex(text)
	if err != nil {
		return Aggregation{}, err
	}
	p := &parser{toks: toks}
	return p.aggregation()
}

// Names are the fields an aggregation uses, each once, in the order
// written.
func (a Aggregation) Names() []string {
	var out []string
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	add(a.Field)
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		add(n.Field)
		for _, k := range n.Kids {
			walk(k)
		}
	}
	walk(a.Where)
	return out
}

// Reading is what an aggregation says, in words.
func (a Aggregation) Reading() string {
	var s string
	switch {
	case a.Func == "COUNT" && a.Field == "":
		s = "the number of entries"
	case a.Func == "COUNT" && a.Distinct:
		s = "the number of distinct " + a.Field
	case a.Func == "COUNT":
		s = "the number of entries with a " + a.Field
	case a.Func == "SUM":
		s = "the sum of " + a.Field
	case a.Func == "AVG":
		s = "the average " + a.Field
	case a.Func == "MIN":
		s = "the smallest " + a.Field
	case a.Func == "MAX":
		s = "the largest " + a.Field
	}
	if a.Func != "COUNT" && a.Distinct {
		s += ", each value once"
	}
	if a.Where != nil {
		s += ", where " + reading(a.Where, false)
	}
	return s
}

func reading(n *Node, inner bool) string {
	switch n.Op {
	case "and", "or":
		parts := make([]string, len(n.Kids))
		for i, k := range n.Kids {
			parts[i] = reading(k, true)
		}
		s := strings.Join(parts, " "+n.Op+" ")
		if inner {
			return "(" + s + ")"
		}
		return s
	case "not":
		return "not " + reading(n.Kids[0], true)
	}
	is := "is "
	if n.Not {
		is = "is not "
	}
	switch n.Op {
	case "=":
		return n.Field + " is " + n.Values[0]
	case "!=":
		return n.Field + " is not " + n.Values[0]
	case "<":
		return n.Field + " is below " + n.Values[0]
	case "<=":
		return n.Field + " is at most " + n.Values[0]
	case ">":
		return n.Field + " is above " + n.Values[0]
	case ">=":
		return n.Field + " is at least " + n.Values[0]
	case "in":
		return n.Field + " " + is + "one of " + strings.Join(n.Values, ", ")
	case "between":
		return n.Field + " " + is + "between " + n.Values[0] + " and " + n.Values[1]
	case "like":
		if n.Not {
			return n.Field + " does not match " + n.Values[0]
		}
		return n.Field + " matches " + n.Values[0]
	case "null":
		return n.Field + " is empty"
	case "notnull":
		return n.Field + " is filled in"
	}
	return ""
}

// Measure is an aggregation as a semantic layer measure: the aggregation
// in the layer's words (count, count_distinct, sum, average, min, max)
// and the expression it aggregates, the condition folded in as a CASE
// WHEN, since a measure has no filter of its own.
func (a Aggregation) Measure() (agg, expr string) {
	agg = map[string]string{"COUNT": "count", "SUM": "sum", "AVG": "average", "MIN": "min", "MAX": "max"}[a.Func]
	if a.Distinct {
		agg = "count_distinct"
	}
	expr = a.Field
	if expr == "" {
		expr = "1"
	}
	if a.Where != nil {
		expr = "case when " + sqlOf(a.Where) + " then " + expr + " end"
	}
	return agg, expr
}

func sqlOf(n *Node) string {
	switch n.Op {
	case "and", "or":
		parts := make([]string, len(n.Kids))
		for i, k := range n.Kids {
			parts[i] = sqlOf(k)
			if k.Op == "and" || k.Op == "or" {
				parts[i] = "(" + parts[i] + ")"
			}
		}
		return strings.Join(parts, " "+n.Op+" ")
	case "not":
		return "not (" + sqlOf(n.Kids[0]) + ")"
	}
	not := ""
	if n.Not {
		not = "not "
	}
	switch n.Op {
	case "in":
		return n.Field + " " + not + "in (" + strings.Join(n.Values, ", ") + ")"
	case "between":
		return n.Field + " " + not + "between " + n.Values[0] + " and " + n.Values[1]
	case "like":
		return n.Field + " " + not + "like " + n.Values[0]
	case "null":
		return n.Field + " is null"
	case "notnull":
		return n.Field + " is not null"
	}
	return n.Field + " " + n.Op + " " + n.Values[0]
}

// --- reading the text ----------------------------------------------------

type kind int

const (
	tIdent kind = iota
	tNumber
	tString
	tSymbol
	tEnd
)

type token struct {
	kind kind
	text string
	at   int
}

// upper is a word as a keyword is compared.
func (t token) upper() string { return strings.ToUpper(t.text) }

// notAggregation are the words of a query rather than an aggregation:
// what a warehouse model says, not a metric.
var notAggregation = map[string]bool{
	"SELECT": true, "FROM": true, "JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true, "FULL": true, "CROSS": true, "ON": true,
	"GROUP": true, "BY": true, "HAVING": true, "ORDER": true, "LIMIT": true, "UNION": true, "WITH": true, "OVER": true, "PARTITION": true,
	"INSERT": true, "UPDATE": true, "DELETE": true,
}

var keywords = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true, "DISTINCT": true, "WHERE": true, "AND": true, "OR": true,
	"NOT": true, "IN": true, "IS": true, "NULL": true, "BETWEEN": true, "LIKE": true, "TRUE": true, "FALSE": true,
}

func lex(s string) ([]token, error) {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isLetter(c):
			j := i
			for j < len(s) && (isLetter(s[j]) || isDigit(s[j])) {
				j++
			}
			out = append(out, token{tIdent, s[i:j], i})
			i = j
		case isDigit(c):
			j := i
			for j < len(s) && (isDigit(s[j]) || s[j] == '.') {
				j++
			}
			if _, err := strconv.ParseFloat(s[i:j], 64); err != nil {
				return nil, Problem{Message: fmt.Sprintf("%s is not a number.", s[i:j]), At: i}
			}
			out = append(out, token{tNumber, s[i:j], i})
			i = j
		case c == '\'':
			j := i + 1
			for {
				if j >= len(s) {
					return nil, Problem{Message: "A value in quotes is not closed: end it with '.", At: i}
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			out = append(out, token{tString, s[i : j+1], i})
			i = j + 1
		case c == '"' || c == '`':
			return nil, Problem{Message: "Name a field as it is, without quotes; put quotes ' ' around a value.", At: i}
		case c == ';':
			return nil, Problem{Message: "A metric is one aggregation: leave out the ;.", At: i}
		default:
			for _, sym := range []string{"<=", ">=", "!=", "<>", "(", ")", ",", "*", "=", "<", ">", "."} {
				if strings.HasPrefix(s[i:], sym) {
					text := sym
					if sym == "<>" {
						text = "!="
					}
					out = append(out, token{tSymbol, text, i})
					i += len(sym)
					goto next
				}
			}
			return nil, Problem{Message: fmt.Sprintf("%q is not part of an aggregation.", string(c)), At: i}
		next:
		}
	}
	return append(out, token{tEnd, "", len(s)}), nil
}

func isLetter(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEnd {
		p.i++
	}
	return t
}

func (p *parser) is(word string) bool {
	t := p.peek()
	return (t.kind == tIdent && t.upper() == word) || (t.kind == tSymbol && t.text == word)
}

func (p *parser) expect(word, want string) error {
	if !p.is(word) {
		return p.wanted(want)
	}
	p.next()
	return nil
}

// wanted is the problem at the next token: what was expected, or why the
// word there does not belong.
func (p *parser) wanted(want string) error {
	t := p.peek()
	if t.kind == tIdent && notAggregation[t.upper()] {
		return Problem{Message: fmt.Sprintf("%s is not part of a metric: a metric is one aggregation over this source's entries. Leave tables, joins and other sources to the warehouse model.", t.upper()), At: t.at}
	}
	if t.kind == tEnd {
		return Problem{Message: "It ends early: " + want + " is missing.", At: t.at}
	}
	return Problem{Message: fmt.Sprintf("Expected %s, not %s.", want, t.text), At: t.at}
}

func (p *parser) aggregation() (Aggregation, error) {
	var a Aggregation
	t := p.peek()
	if t.kind != tIdent || !map[string]bool{"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true}[t.upper()] {
		if t.kind == tIdent && !notAggregation[t.upper()] {
			return a, Problem{Message: fmt.Sprintf("%s is not an aggregate: start with COUNT, COUNT(DISTINCT …), SUM, AVG, MIN or MAX.", t.text), At: t.at}
		}
		return a, p.wanted("an aggregate function (COUNT, SUM, AVG, MIN or MAX)")
	}
	a.Func = p.next().upper()
	if err := p.expect("(", "( after "+a.Func); err != nil {
		return a, err
	}
	if p.is("DISTINCT") {
		p.next()
		a.Distinct = true
	}
	if p.is("*") {
		at := p.next().at
		if a.Func != "COUNT" || a.Distinct {
			return a, Problem{Message: fmt.Sprintf("%s(*) has nothing to add up: name the field, such as %s(weight_kg). Only COUNT(*) counts every entry.", a.Func, a.Func), At: at}
		}
	} else {
		f, err := p.field()
		if err != nil {
			return a, err
		}
		a.Field = f
	}
	if err := p.expect(")", ") to close "+a.Func); err != nil {
		return a, err
	}
	if p.is("WHERE") {
		p.next()
		w, err := p.or()
		if err != nil {
			return a, err
		}
		a.Where = w
	}
	if p.peek().kind != tEnd {
		return a, p.wanted("the end, or WHERE and a condition")
	}
	return a, nil
}

// field is a name of the source's own: a dotted name reads another table.
func (p *parser) field() (string, error) {
	t := p.peek()
	if t.kind != tIdent || keywords[t.upper()] || notAggregation[t.upper()] {
		return "", p.wanted("a field")
	}
	p.next()
	if p.is(".") {
		dot := p.next()
		rest := p.peek().text
		return "", Problem{Message: fmt.Sprintf("%s.%s reads another table: name the field as this source records it, such as %s.", t.text, rest, rest), At: dot.at}
	}
	return t.text, nil
}

func (p *parser) or() (*Node, error) {
	n, err := p.and()
	if err != nil {
		return nil, err
	}
	if !p.is("OR") {
		return n, nil
	}
	out := &Node{Op: "or", Kids: []*Node{n}}
	for p.is("OR") {
		p.next()
		k, err := p.and()
		if err != nil {
			return nil, err
		}
		out.Kids = append(out.Kids, k)
	}
	return out, nil
}

func (p *parser) and() (*Node, error) {
	n, err := p.factor()
	if err != nil {
		return nil, err
	}
	if !p.is("AND") {
		return n, nil
	}
	out := &Node{Op: "and", Kids: []*Node{n}}
	for p.is("AND") {
		p.next()
		k, err := p.factor()
		if err != nil {
			return nil, err
		}
		out.Kids = append(out.Kids, k)
	}
	return out, nil
}

func (p *parser) factor() (*Node, error) {
	switch {
	case p.is("NOT"):
		p.next()
		k, err := p.factor()
		if err != nil {
			return nil, err
		}
		return &Node{Op: "not", Kids: []*Node{k}}, nil
	case p.is("("):
		p.next()
		if p.is("SELECT") {
			return nil, p.wanted("a condition")
		}
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")", ") to close the condition"); err != nil {
			return nil, err
		}
		return n, nil
	}
	return p.comparison()
}

func (p *parser) comparison() (*Node, error) {
	f, err := p.field()
	if err != nil {
		return nil, err
	}
	n := &Node{Field: f}
	if p.is("IS") {
		p.next()
		n.Op = "null"
		if p.is("NOT") {
			p.next()
			n.Op = "notnull"
		}
		return n, p.expect("NULL", "NULL after IS")
	}
	if p.is("NOT") {
		p.next()
		n.Not = true
		if !p.is("IN") && !p.is("BETWEEN") && !p.is("LIKE") {
			return nil, p.wanted("IN, BETWEEN or LIKE after NOT")
		}
	}
	switch t := p.peek(); {
	case p.is("IN"):
		p.next()
		n.Op = "in"
		if err := p.expect("(", "( and the values"); err != nil {
			return nil, err
		}
		for {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.Values = append(n.Values, v)
			if !p.is(",") {
				break
			}
			p.next()
		}
		return n, p.expect(")", ") to close the values")
	case p.is("BETWEEN"):
		p.next()
		n.Op = "between"
		lo, err := p.value()
		if err != nil {
			return nil, err
		}
		if err := p.expect("AND", "AND between the two values"); err != nil {
			return nil, err
		}
		hi, err := p.value()
		if err != nil {
			return nil, err
		}
		n.Values = []string{lo, hi}
		return n, nil
	case p.is("LIKE"):
		p.next()
		n.Op = "like"
		v := p.peek()
		if v.kind != tString {
			return nil, p.wanted("a value in quotes after LIKE")
		}
		p.next()
		n.Values = []string{v.text}
		return n, nil
	case t.kind == tSymbol && map[string]bool{"=": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}[t.text] && !n.Not:
		p.next()
		n.Op = t.text
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		n.Values = []string{v}
		return n, nil
	}
	return nil, p.wanted("a comparison (=, !=, <, <=, >, >=, IN, BETWEEN, LIKE or IS NULL) after " + f)
}

func (p *parser) value() (string, error) {
	t := p.peek()
	switch {
	case t.kind == tNumber, t.kind == tString:
		p.next()
		return t.text, nil
	case t.kind == tIdent && (t.upper() == "TRUE" || t.upper() == "FALSE"):
		p.next()
		return strings.ToLower(t.text), nil
	case t.kind == tIdent && !keywords[t.upper()] && !notAggregation[t.upper()]:
		return "", Problem{Message: fmt.Sprintf("Compare with a value: put text in quotes, such as '%s'. A field is not compared with another field in a metric.", t.text), At: t.at}
	}
	return "", p.wanted("a value (a number, or text in quotes)")
}
