// funclit keeps Go code laid out the way this repository wants it. It reports:
//
//   - function literals whose body is written on one line
//   - composite literals whose elements share a line: struct literals, those
//     holding a function literal, and map literals of more than one entry
//   - multi-line blocks with no blank line around them
//   - composite literals written in a range header
//   - init statements in if and switch headers, but for the idioms
//     v, ok := x; ok and err := f(); err != nil
//   - parallel assignments, a, b := x, y, but for those where one side reads
//     the other, such as a swap
//   - an else after an if whose body always leaves the block
//   - lines wider than maxWidth columns, split at the top-level && or || of an
//     if condition, or between the arguments, elements or parameters of the
//     outermost call, literal or signature on the line
//
// With -fix it rewrites what it can and gofmts the file, going round again
// until nothing changes, since one fix can leave work for another. What it
// cannot rewrite safely it still reports.
//
// A block is an if, for, switch, select or bare { }. It gets a blank line
// after it when a statement follows, and before it unless the statement above
// sets a variable its header reads, so v, err := f() stays with if err != nil.
//
// A range literal moves to a variable named for the loop variable, tokens for
// token. An init statement moves to the line above. Either is left alone when
// the name it would bring into scope is already used nearby.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"rd/utils"
)

var skipDirs = map[string]bool{
	"vendor":      true,
	"target":      true,
	".libghostty": true,
	".git":        true,
}

const (
	msgIfInit     = "init statement in an if header; put it on the line above"
	msgSwitchInit = "init statement in a switch header; put it on the line above"
	msgBlankLine  = "block without a blank line around it; put one between it and the statement next to it"
)

// maxPasses bounds how often -fix goes round one file. Each hoist out of a
// function is a pass of its own, and a test file can hold dozens.
const maxPasses = 200

func main() {
	fix := flag.Bool("fix", false, "rewrite offending code in place")
	flag.Parse()

	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	failed := false

	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				if path != root && skipDirs[d.Name()] {
					return filepath.SkipDir
				}

				return nil
			}

			if !strings.HasSuffix(path, ".go") {
				return nil
			}

			if !check(path, *fix) {
				failed = true
			}

			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if failed {
		os.Exit(1)
	}
}

// A finding is one thing reported, and whether -fix has an edit for it.
type finding struct {
	pos     token.Position
	msg     string
	fixable bool
}

// An edit replaces src[at:end] with text; at == end inserts.
type edit struct {
	at, end int
	text    string
}

// check reports whether path is clean, after fixing it when fix is set.
func check(path string, fix bool) bool {
	orig, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return false
	}

	src := orig

	for pass := 0; ; pass++ {
		found, edits, err := scan(path, src)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return false
		}

		if fix && len(edits) > 0 && pass < maxPasses {
			src, err = apply(src, edits)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				return false
			}

			continue
		}

		if fix && !bytes.Equal(src, orig) {
			if err := os.WriteFile(path, src, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return false
			}
		}

		for _, f := range found {
			if !fix || !f.fixable || pass == maxPasses {
				fmt.Printf("%s:%d:%d: %s\n", path, f.pos.Line, f.pos.Column, f.msg)
			}
		}

		return len(found) == 0
	}
}

// apply makes the edits and gofmts the result. An edit inside the span another
// replaces is dropped; the next pass finds it again in the rewritten source.
func apply(src []byte, edits []edit) ([]byte, error) {
	slices.SortStableFunc(edits, func(a, b edit) int {
		return a.at - b.at
	})
	edits = slices.Compact(edits)

	out := make([]byte, 0, len(src)+64*len(edits))
	prev := 0

	for _, ed := range edits {
		if ed.at < prev {
			continue
		}

		out = append(append(out, src[prev:ed.at]...), ed.text...)
		prev = ed.end
	}

	return format.Source(append(out, src[prev:]...))
}

// scan finds what is wrong with src and the edits that fix it. When a pass has
// hoists, they are its only edits: a hoist moves code that the other edits
// were placed against.
func scan(path string, src []byte) ([]finding, []edit, error) {
	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}

	s := &scanner{
		fset:    fset,
		file:    f,
		src:     src,
		hoisted: map[ast.Node]bool{},
	}
	s.literals()
	s.blankLines()
	s.longLines()

	if len(s.hoists) > 0 {
		return s.found, s.hoists, nil
	}

	return s.found, s.edits, nil
}

type scanner struct {
	fset  *token.FileSet
	file  *ast.File
	src   []byte
	found []finding

	edits  []edit
	hoists []edit
	// hoisted holds the functions with a hoist this pass: two in one function
	// could bring the same name into the same scope
	hoisted map[ast.Node]bool
}

func (s *scanner) report(p token.Pos, fixable bool, msg string) {
	s.found = append(s.found, finding{
		s.fset.Position(p),
		msg,
		fixable,
	})
}

func (s *scanner) line(p token.Pos) int {
	return s.fset.Position(p).Line
}

func (s *scanner) offset(p token.Pos) int {
	return s.fset.Position(p).Offset
}

func (s *scanner) text(n ast.Node) string {
	return string(s.src[s.offset(n.Pos()):s.offset(n.End())])
}

// literals walks the file for the rules that look at one node and where it
// sits: function literal bodies, crowded literals, range literals and init
// statements.
func (s *scanner) literals() {
	var stack []ast.Node
	var marked []*ast.CompositeLit

	seen := map[*ast.CompositeLit]bool{}
	mark := func(cl *ast.CompositeLit) {
		if !seen[cl] {
			seen[cl] = true
			marked = append(marked, cl)
		}
	}

	ast.Inspect(s.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}

		stack = append(stack, n)

		switch n := n.(type) {
		case *ast.CompositeLit:
			switch litType(stack).(type) {
			case *ast.MapType:
				if len(n.Elts) > 1 {
					mark(n)
				}
			case *ast.ArrayType, nil:
			default:
				if fieldKeys(n) {
					mark(n)
				}
			}
		case *ast.FuncLit:
			if len(n.Body.List) == 0 {
				break
			}

			for _, cl := range enclosingLits(stack) {
				mark(cl)
			}

			s.funcBody(n)
		case *ast.RangeStmt:
			s.rangeLit(n, stack)
		case *ast.IfStmt:
			if n.Init != nil && !idiom(n) {
				s.hoistInit(n, n.Init, n.Cond.Pos(), stack, msgIfInit)
			}

			s.earlyExit(n, stack)
		case *ast.AssignStmt:
			s.parallel(n, stack)
		case *ast.SwitchStmt:
			if n.Init != nil {
				next := n.Body.Lbrace

				if n.Tag != nil {
					next = n.Tag.Pos()
				}

				s.hoistInit(n, n.Init, next, stack, msgSwitchInit)
			}
		case *ast.TypeSwitchStmt:
			if n.Init != nil {
				s.hoistInit(n, n.Init, n.Assign.Pos(), stack, msgSwitchInit)
			}
		}

		return true
	})

	for _, cl := range marked {
		s.spread(cl)
	}
}

func (s *scanner) funcBody(lit *ast.FuncLit) {
	open := lit.Body.Lbrace
	end := lit.Body.Rbrace
	if s.line(open) != s.line(end) {
		return
	}

	s.report(open, true, "function literal body on one line; put the body on its own line")
	s.edits = append(s.edits,
		edit{
			s.offset(open) + 1,
			s.offset(open) + 1,
			"\n",
		},
		edit{
			s.offset(end),
			s.offset(end),
			"\n",
		},
	)
}

// spread gives each element of cl a line of its own.
func (s *scanner) spread(cl *ast.CompositeLit) {
	if s.split(cl.Lbrace, cl.Rbrace, exprNodes(cl.Elts)) {
		s.report(cl.Lbrace, true, "composite literal elements share a line; put each element on its own line")
	}
}

// split gives each of items, between the brackets open and close, a line of
// its own, and the closing bracket one too. It reports whether that took any
// edits.
func (s *scanner) split(open, close token.Pos, items []ast.Node) bool {
	if len(items) == 0 {
		return false
	}

	before := len(s.edits)
	prev := open

	for _, it := range items {
		if s.line(it.Pos()) == s.line(prev) {
			at := s.offset(it.Pos())
			s.edits = append(s.edits, edit{
				at,
				at,
				"\n",
			})
		}

		prev = it.End()
	}

	if s.line(close) == s.line(prev) {
		// a newline before the bracket needs a comma after the last item
		text := ",\n"
		at := s.offset(close)

		if bytes.Contains(s.src[s.offset(prev):at], []byte(",")) {
			text = "\n"
		}

		s.edits = append(s.edits, edit{
			at,
			at,
			text,
		})
	}

	return len(s.edits) > before
}

func exprNodes(xs []ast.Expr) []ast.Node {
	out := make([]ast.Node, len(xs))

	for i, x := range xs {
		out[i] = x
	}

	return out
}

func fieldNodes(fs []*ast.Field) []ast.Node {
	out := make([]ast.Node, len(fs))

	for i, f := range fs {
		out[i] = f
	}

	return out
}

// parallel puts each pair of a, b := x, y on a line of its own. One where a
// right side reads a name on the left is left alone, a swap being the common
// one: split, the second line would read what the first just wrote.
func (s *scanner) parallel(as *ast.AssignStmt, stack []ast.Node) {
	if len(as.Lhs) < 2 || len(as.Lhs) != len(as.Rhs) {
		return
	}

	list := enclosingList(stack)
	if list == nil {
		return // a for or if header, where one statement is all there is room for
	}

	var names []string

	for _, x := range as.Lhs {
		id, ok := x.(*ast.Ident)
		if !ok {
			return
		}

		if id.Name != "_" {
			names = append(names, id.Name)
		}
	}

	for _, x := range as.Rhs {
		for _, name := range names {
			if uses(x, name) {
				return
			}
		}
	}

	const msg = "parallel assignment; put each assignment on its own line"

	// a := that redeclares a name only works alongside a new one, which a
	// line of its own may not have
	if as.Tok == token.DEFINE && declaredBefore(as, list, enclosingFunc(stack), names) {
		s.report(as.Pos(), false, msg)
		return
	}

	lines := make([]string, len(as.Lhs))

	for i := range as.Lhs {
		tok := as.Tok.String()

		if isIdent(as.Lhs[i], "_") {
			tok = "="
		}

		lines[i] = s.text(as.Lhs[i]) + " " + tok + " " + s.text(as.Rhs[i])
	}

	s.report(as.Pos(), true, msg)
	s.edits = append(s.edits, edit{
		s.offset(as.Pos()),
		s.offset(as.End()),
		strings.Join(lines, "\n"),
	})
}

// declaredBefore is whether any of names appears before stmt in list, or in
// the signature of fn, either of which could make a := of it a redeclaration.
func declaredBefore(stmt ast.Stmt, list []ast.Stmt, fn ast.Node, names []string) bool {
	var sig ast.Node

	switch fn := fn.(type) {
	case *ast.FuncDecl:
		sig = fn.Type

		if fn.Recv != nil {
			for _, name := range names {
				if uses(fn.Recv, name) {
					return true
				}
			}
		}
	case *ast.FuncLit:
		sig = fn.Type
	}

	for _, name := range names {
		if sig != nil && uses(sig, name) {
			return true
		}

		for _, other := range list {
			if other == stmt {
				break
			}

			if uses(other, name) {
				return true
			}
		}
	}

	return false
}

// earlyExit drops the else after an if whose body always leaves the block:
// what follows the if only runs when it did not, so the else only indents.
func (s *scanner) earlyExit(n *ast.IfStmt, stack []ast.Node) {
	if n.Else == nil || !leaves(n.Body) {
		return
	}

	list := enclosingList(stack)
	if list == nil {
		return // an else if, which the if above it lifts out first
	}

	const msg = "else after a branch that leaves the block; drop the else"

	// what the init declares is out of scope once the else is not part of the if
	if as, ok := n.Init.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
		for _, x := range as.Lhs {
			id, ok := x.(*ast.Ident)
			if ok && id.Name != "_" && uses(n.Else, id.Name) {
				s.report(n.Else.Pos(), false, msg)
				return
			}
		}
	}

	text := ""

	switch e := n.Else.(type) {
	case *ast.IfStmt:
		text = s.text(e)
	case *ast.BlockStmt:
		names := declared(e.List)

		for _, other := range list {
			if other == ast.Stmt(n) {
				continue
			}

			for _, name := range names {
				if uses(other, name) {
					s.report(n.Else.Pos(), false, msg)
					return
				}
			}
		}

		if declaredBefore(n, nil, enclosingFunc(stack), names) {
			s.report(n.Else.Pos(), false, msg)
			return
		}

		text = string(s.src[s.offset(e.Lbrace)+1 : s.offset(e.Rbrace)])
	}

	s.report(n.Else.Pos(), true, msg)
	s.edits = append(s.edits, edit{
		s.offset(n.Body.End()),
		s.offset(n.Else.End()),
		"\n\n" + text,
	})
}

// leaves is whether a block always ends by leaving: a return, a break,
// continue or goto, or a panic.
func leaves(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}

	switch last := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return last.Tok != token.FALLTHROUGH
	case *ast.ExprStmt:
		call, ok := last.X.(*ast.CallExpr)
		return ok && isIdent(call.Fun, "panic")
	}

	return false
}

// declared is the names a statement list declares at its own level.
func declared(list []ast.Stmt) []string {
	var names []string

	for _, st := range list {
		switch st := st.(type) {
		case *ast.AssignStmt:
			if st.Tok != token.DEFINE {
				continue
			}

			for _, x := range st.Lhs {
				id, ok := x.(*ast.Ident)
				if ok && id.Name != "_" {
					names = append(names, id.Name)
				}
			}
		case *ast.DeclStmt:
			g, ok := st.Decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			for _, spec := range g.Specs {
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					for _, id := range spec.Names {
						names = append(names, id.Name)
					}
				case *ast.TypeSpec:
					names = append(names, spec.Name.Name)
				}
			}
		case *ast.LabeledStmt:
			names = append(names, st.Label.Name)
		}
	}

	return names
}

// maxWidth is the widest a line of code may be, counting a tab as tabWidth.
const (
	maxWidth = 120
	tabWidth = 4
)

// A splitter is a way to break one long line: the brackets and what is
// between them, or the operators of a condition.
type splitter struct {
	open, close token.Pos
	items       []ast.Node
	ops         []token.Pos // after each of these, for a condition
}

// longLines breaks lines wider than maxWidth. A line with an if condition
// joined by && or || breaks after each; otherwise the outermost call, literal
// or parameter list that opens and closes on the line puts each of its items
// on a line of its own. What is too long with nothing to break is reported.
// Comments, and the inside of a string spanning lines, are not code to break.
func (s *scanner) longLines() {
	best := map[int]splitter{}
	conds := map[int]splitter{}
	var skip utils.Bitset
	commentAt := map[int]int{} // a line's first comment column, where code ends

	offer := func(open, close token.Pos, items []ast.Node) {
		l := s.line(open)
		if len(items) < 2 || s.line(close) != l {
			return
		}

		cur, ok := best[l]
		if !ok || close-open > cur.close-cur.open {
			best[l] = splitter{
				open:  open,
				close: close,
				items: items,
			}
		}
	}

	ast.Inspect(s.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			offer(n.Lparen, n.Rparen, exprNodes(n.Args))
		case *ast.CompositeLit:
			offer(n.Lbrace, n.Rbrace, exprNodes(n.Elts))
		case *ast.FieldList:
			if n.Opening.IsValid() && n.Closing.IsValid() {
				offer(n.Opening, n.Closing, fieldNodes(n.List))
			}
		case *ast.IfStmt:
			ops := condOps(n.Cond)
			if len(ops) > 0 && s.line(n.Cond.Pos()) == s.line(n.Cond.End()) {
				conds[s.line(n.Cond.Pos())] = splitter{
					ops: ops,
				}
			}
		case *ast.BasicLit:
			for l := s.line(n.Pos()) + 1; l <= s.line(n.End()); l++ {
				skip.Set(l)
			}
		}

		return true
	})

	for _, cg := range s.file.Comments {
		for _, c := range cg.List {
			p := s.fset.Position(c.Pos())

			col, ok := commentAt[p.Line]
			if !ok || p.Column < col {
				commentAt[p.Line] = p.Column
			}

			for l := p.Line + 1; l <= s.line(c.End()); l++ {
				skip.Set(l)
			}
		}
	}

	for i, text := range bytes.Split(s.src, []byte("\n")) {
		l := i + 1
		if skip.Has(l) {
			continue
		}

		if col, ok := commentAt[l]; ok {
			text = text[:col-1]
		}

		if width(text) <= maxWidth {
			continue
		}

		pos := s.fset.File(s.file.Pos()).LineStart(l)
		msg := fmt.Sprintf("line wider than %d columns; split it", maxWidth)

		if c, ok := conds[l]; ok {
			for _, op := range c.ops {
				at := s.offset(op)
				s.edits = append(s.edits, edit{
					at,
					at,
					"\n",
				})
			}

			s.report(pos, true, msg)
			continue
		}

		if b, ok := best[l]; ok && s.split(b.open, b.close, b.items) {
			s.report(pos, true, msg)
			continue
		}

		s.report(pos, false, msg+" (nothing to split it at)")
	}
}

// condOps are the positions just after each top-level && or || of a
// condition, when its top-level operator is one of them.
func condOps(cond ast.Expr) []token.Pos {
	top, ok := cond.(*ast.BinaryExpr)
	if !ok || (top.Op != token.LAND && top.Op != token.LOR) {
		return nil
	}

	var ops []token.Pos

	for x := ast.Expr(top); ; {
		be, ok := x.(*ast.BinaryExpr)
		if !ok || be.Op != top.Op {
			break
		}

		ops = append(ops, be.OpPos+token.Pos(len(be.Op.String())))
		x = be.X
	}

	return ops
}

// width is how many columns text takes, a tab counting as tabWidth.
func width(text []byte) int {
	w := 0

	for _, r := range string(text) {
		if r == '\t' {
			w += tabWidth
			continue
		}

		w++
	}

	return w
}

// rangeLit moves a literal out of a range header into a variable above the
// loop, named the plural of the loop variable.
func (s *scanner) rangeLit(rs *ast.RangeStmt, stack []ast.Node) {
	lit, ok := rs.X.(*ast.CompositeLit)
	if !ok {
		return
	}

	const msg = "composite literal in a range header; put it in a variable above the loop"

	value, ok := rs.Value.(*ast.Ident)
	if !ok || value.Name == "_" {
		s.report(lit.Pos(), false, msg)
		return
	}

	name := plural(value.Name)
	fn := enclosingFunc(stack)
	at, ok := stmtStart(stack)

	if fn == nil || !ok || s.hoisted[fn] || uses(fn, name) {
		s.report(lit.Pos(), false, msg+" (as "+name+")")
		return
	}

	s.hoisted[fn] = true
	s.report(lit.Pos(), true, msg)
	s.hoists = append(s.hoists,
		edit{
			s.offset(at),
			s.offset(at),
			name + " := " + s.text(lit) + "\n",
		},
		edit{
			s.offset(lit.Pos()),
			s.offset(lit.End()),
			name,
		},
	)
}

// hoistInit moves an if or switch statement's init to the line above it. next
// is what follows the init in the header, so the init and its semicolon go.
func (s *scanner) hoistInit(stmt ast.Stmt, init ast.Stmt, next token.Pos, stack []ast.Node, msg string) {
	fn := enclosingFunc(stack)
	at, ok := stmtStart(stack)
	list := enclosingList(stack)

	if fn == nil || !ok || s.hoisted[fn] || clashes(init, stmt, list) {
		s.report(init.Pos(), false, msg)
		return
	}

	s.hoisted[fn] = true
	s.report(init.Pos(), true, msg)
	s.hoists = append(s.hoists,
		edit{
			s.offset(at),
			s.offset(at),
			s.text(init) + "\n",
		},
		edit{
			s.offset(init.Pos()),
			s.offset(next),
			"",
		},
	)
}

// blankLines puts a blank line around each multi-line block.
func (s *scanner) blankLines() {
	tf := s.fset.File(s.file.Pos())

	ast.Inspect(s.file, func(n ast.Node) bool {
		var list []ast.Stmt

		switch n := n.(type) {
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		case *ast.CommClause:
			list = n.Body
		}

		for i := 1; i < len(list); i++ {
			a := list[i-1]
			b := list[i]
			after := isBlock(s.fset, a)
			before := isBlock(s.fset, b) && !cuddled(a, b)

			if !after && !before {
				continue
			}

			next := s.line(a.End()) + 1
			if s.line(firstAfter(s.fset, s.file, a.End(), b.Pos())) != next {
				continue // a blank line, or a and b on one line, which gofmt settles
			}

			at := tf.Offset(tf.LineStart(next))
			s.report(b.Pos(), true, msgBlankLine)
			s.edits = append(s.edits, edit{
				at,
				at,
				"\n",
			})
		}

		return true
	})
}

// litType is the type of the composite literal at the end of stack, taken
// from the literal around it when elided, as in []T{{...}}. A struct literal
// is one whose type is neither an array, slice nor map.
func litType(stack []ast.Node) ast.Expr {
	cl := stack[len(stack)-1].(*ast.CompositeLit)
	if cl.Type != nil {
		return cl.Type
	}

	var child ast.Node = cl

	for i := len(stack) - 2; i >= 0; i-- {
		switch parent := stack[i].(type) {
		case *ast.UnaryExpr:
			child = parent
			continue
		case *ast.KeyValueExpr:
			if i == 0 {
				return nil
			}

			if _, ok := stack[i-1].(*ast.CompositeLit); !ok {
				return nil
			}

			return elided(litType(stack[:i]), parent.Key == child)
		case *ast.CompositeLit:
			return elided(litType(stack[:i+1]), false)
		default:
			return nil
		}
	}

	return nil
}

// fieldKeys reports whether every key in cl is an identifier, as a struct's
// are, so a named map type such as M{1: x} is not taken for a struct.
func fieldKeys(cl *ast.CompositeLit) bool {
	for _, e := range cl.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		if _, ok := kv.Key.(*ast.Ident); !ok {
			return false
		}
	}

	return true
}

// elided is the type an element of a literal of type t takes when its own
// type is left out: a map's key or value, or an array's element.
func elided(t ast.Expr, key bool) ast.Expr {
	var out ast.Expr

	switch t := t.(type) {
	case *ast.ArrayType:
		out = t.Elt
	case *ast.MapType:
		out = t.Value

		if key {
			out = t.Key
		}
	default:
		return nil
	}

	if star, ok := out.(*ast.StarExpr); ok {
		return star.X
	}

	return out
}

// enclosingLits are the composite literals a function literal, the last node
// of stack, is an element of: its own, and each one that literal is an element
// of in turn, so a struct in a slice is laid out like the slice around it.
func enclosingLits(stack []ast.Node) []*ast.CompositeLit {
	var out []*ast.CompositeLit

	child := stack[len(stack)-1]

	for i := len(stack) - 2; i >= 0; i-- {
		switch parent := stack[i].(type) {
		case *ast.KeyValueExpr:
			if parent.Value != child {
				return out
			}
		case *ast.CompositeLit:
			elt, ok := child.(ast.Expr)
			if !ok || !slices.Contains(parent.Elts, elt) {
				return out
			}

			out = append(out, parent)
		default:
			return out
		}

		child = stack[i]
	}

	return out
}

// enclosingFunc is the function the last node of stack is in.
func enclosingFunc(stack []ast.Node) ast.Node {
	for i := len(stack) - 2; i >= 0; i-- {
		switch stack[i].(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return stack[i]
		}
	}

	return nil
}

// stmtStart is where a statement can go just before the last node of stack:
// before its label when it has one. It is false when the node is not in a
// statement list, such as the if of an else if.
func stmtStart(stack []ast.Node) (token.Pos, bool) {
	if enclosingList(stack) == nil {
		return token.NoPos, false
	}

	start := stack[len(stack)-1].Pos()

	for i := len(stack) - 2; i >= 0; i-- {
		l, ok := stack[i].(*ast.LabeledStmt)
		if !ok {
			break
		}

		start = l.Pos()
	}

	return start, true
}

// enclosingList is the statement list the last node of stack is in, or nil.
func enclosingList(stack []ast.Node) []ast.Stmt {
	if len(stack) < 2 {
		return nil
	}

	stmt, ok := stack[len(stack)-1].(ast.Stmt)
	if !ok {
		return nil
	}

	var list []ast.Stmt

	switch p := stack[len(stack)-2].(type) {
	case *ast.BlockStmt:
		list = p.List
	case *ast.CaseClause:
		list = p.Body
	case *ast.CommClause:
		list = p.Body
	case *ast.LabeledStmt:
		return enclosingList(stack[:len(stack)-1])
	}

	if !slices.Contains(list, stmt) {
		return nil
	}

	return list
}

// idiom is whether an if's init is one kept in the header: v, ok := x; ok
// (or !ok, or ok && more), or err := f(); err != nil.
func idiom(n *ast.IfStmt) bool {
	as, ok := n.Init.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return false
	}

	cond := n.Cond

	for {
		be, ok := cond.(*ast.BinaryExpr)
		if !ok || be.Op != token.LAND {
			break
		}

		cond = be.X
	}

	u, ok := cond.(*ast.UnaryExpr)
	if ok && u.Op == token.NOT {
		cond = u.X
	}

	id, ok := cond.(*ast.Ident)
	if ok && len(as.Lhs) == 2 {
		return isIdent(as.Lhs[1], id.Name) && id.Name != "_"
	}

	be, ok := n.Cond.(*ast.BinaryExpr)
	if !ok || (be.Op != token.NEQ && be.Op != token.EQL) || !isIdent(be.X, "err") || !isIdent(be.Y, "nil") {
		return false
	}

	hasErr := false

	for _, x := range as.Lhs {
		switch {
		case isIdent(x, "err"):
			hasErr = true
		case !isIdent(x, "_"):
			return false
		}
	}

	return hasErr
}

func isIdent(x ast.Expr, name string) bool {
	id, ok := x.(*ast.Ident)
	return ok && id.Name == name
}

// clashes is whether moving init out of stmt's header would change what a
// name means: it declares a name used elsewhere in the list stmt is in, which
// would then see it, or be redeclared by it.
func clashes(init, stmt ast.Stmt, list []ast.Stmt) bool {
	as, ok := init.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return false
	}

	for _, other := range list {
		if other == stmt {
			continue
		}

		for _, x := range as.Lhs {
			id, ok := x.(*ast.Ident)
			if ok && id.Name != "_" && uses(other, id.Name) {
				return true
			}
		}
	}

	return false
}

// uses is whether name appears anywhere under n.
func uses(n ast.Node, name string) bool {
	found := false

	ast.Inspect(n, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && id.Name == name {
			found = true
		}

		return !found
	})

	return found
}

// esSuffixes are the endings that take -es in the plural.
var esSuffixes = []string{"s", "x", "z", "ch", "sh"}

// plural is the name for a list of name: token gives tokens, box boxes, entry
// entries.
func plural(name string) string {
	for _, suffix := range esSuffixes {
		if strings.HasSuffix(name, suffix) {
			return name + "es"
		}
	}

	if len(name) > 1 && strings.HasSuffix(name, "y") && !strings.ContainsRune("aeiou", rune(name[len(name)-2])) {
		return name[:len(name)-1] + "ies"
	}

	return name + "s"
}

// isBlock is whether s is a block statement spanning more than one line.
func isBlock(fset *token.FileSet, s ast.Stmt) bool {
	if l, ok := s.(*ast.LabeledStmt); ok {
		s = l.Stmt
	}

	switch s.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
		*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.BlockStmt:
		return fset.Position(s.Pos()).Line != fset.Position(s.End()).Line
	}

	return false
}

// cuddled is whether a sets a variable the header of block b reads.
func cuddled(a, b ast.Stmt) bool {
	set := map[string]bool{}

	switch a := a.(type) {
	case *ast.AssignStmt:
		for _, x := range a.Lhs {
			addIdents(set, x)
		}
	case *ast.IncDecStmt:
		addIdents(set, a.X)
	case *ast.DeclStmt:
		g, ok := a.Decl.(*ast.GenDecl)
		if !ok {
			break
		}

		for _, spec := range g.Specs {
			v, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for _, name := range v.Names {
				set[name.Name] = true
			}
		}
	}

	if len(set) == 0 {
		return false
	}

	if l, ok := b.(*ast.LabeledStmt); ok {
		b = l.Stmt
	}

	var header []ast.Node

	switch b := b.(type) {
	case *ast.IfStmt:
		header = []ast.Node{b.Init, b.Cond}
	case *ast.ForStmt:
		header = []ast.Node{b.Init, b.Cond, b.Post}
	case *ast.RangeStmt:
		header = []ast.Node{b.Key, b.Value, b.X}
	case *ast.SwitchStmt:
		header = []ast.Node{b.Init, b.Tag}
	case *ast.TypeSwitchStmt:
		header = []ast.Node{b.Init, b.Assign}
	}

	used := map[string]bool{}

	for _, h := range header {
		if h != nil {
			addIdents(used, h)
		}
	}

	for name := range set {
		if used[name] {
			return true
		}
	}

	return false
}

// addIdents adds every identifier under n to names.
func addIdents(names map[string]bool, n ast.Node) {
	ast.Inspect(n, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && id.Name != "_" {
			names[id.Name] = true
		}

		return true
	})
}

// firstAfter is the first thing on a line below from: a comment above to, so
// the comment moves with the block, or to itself. A comment trailing from's
// own line is not one.
func firstAfter(fset *token.FileSet, f *ast.File, from, to token.Pos) token.Pos {
	below := fset.Position(from).Line
	i := sort.Search(len(f.Comments), func(i int) bool {
		return f.Comments[i].Pos() > from
	})

	for ; i < len(f.Comments) && f.Comments[i].Pos() < to; i++ {
		if fset.Position(f.Comments[i].Pos()).Line > below {
			return f.Comments[i].Pos()
		}
	}

	return to
}
