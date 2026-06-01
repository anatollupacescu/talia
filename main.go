// talia — exhaustive execution-path title generator for annotated Go functions.
//
// Usage:
//
//	go run main.go <source.go>
//
// Annotations are placed in single-line comments anywhere in the source file:
//
// talia:fun(name)     opens a function scope
// talia:pre(label)    precondition guard — legal only at function top-level, before logic
// talia:cond(label)   opens a conditional scope
// talia:break(label)  conditional loop exit — legal only inside an enclosing loop
// talia:loop(label)   opens a loop scope
// talia:done          closes the nearest open cond or loop
// talia:return(label) explicit function exit — legal anywhere inside a function

package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: talia <source.go>")
		os.Exit(1)
	}

	file, err := parse(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Print(toTestFile(file))
}

// =============================================================================
// NODE TYPES
// =============================================================================

// kind identifies the structural role of an annotated node.
type kind int

const (
	kindFunc   kind = iota // function boundary              talia:fun
	kindPre                // precondition guard             talia:pre
	kindCond               // conditional scope (has body)   talia:cond … talia:done
	kindBreak              // conditional loop exit (leaf)   talia:break
	kindLoop               // loop scope (has body)          talia:loop … talia:done
	kindReturn             // explicit function exit (leaf)  talia:return
)

func (k kind) String() string {
	return [...]string{"func", "pre", "cond", "break", "loop", "return"}[k]
}

// node is one annotated control-flow event.
// kindFunc, kindCond, and kindLoop carry a children slice (the scope body).
// kindPre, kindBreak, and kindReturn are leaves with no children.
type node struct {
	k        kind
	label    string
	children []*node
}

// fileNode is the root of the annotation graph for one source file.
type fileNode struct {
	name  string  // source-file path
	funcs []*node // kindFunc nodes in declaration order
}

func (f *fileNode) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", f.name)
	for _, fn := range f.funcs {
		fmt.Fprintf(&sb, "* %s\n", fn.label)
		root := newTrie()
		for _, path := range paths(fn) {
			root.insert(path)
		}
		root.writeTo(&sb, 1)
	}
	return sb.String()
}

func toTestFile(file *fileNode) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "package main_test\n\nimport \"testing\"\n\n")
	for _, fn := range file.funcs {
		root := newTrie()
		for _, path := range paths(fn) {
			root.insert(path)
		}
		name := "Test" + strings.ToUpper(fn.label[:1]) + fn.label[1:]
		fmt.Fprintf(&sb, "func %s(t *testing.T) {\n", name)
		writeTestTrie(&sb, root, 1)
		fmt.Fprintf(&sb, "}\n\n")
	}
	fmt.Fprint(&sb, mustPanicHelper)
	return sb.String()
}

func writeTestTrie(sb *strings.Builder, t *trie, depth int) {
	indent := strings.Repeat("\t", depth)
	for _, child := range t.children {
		fmt.Fprintf(sb, "%st.Run(%q, func(t *testing.T) {\n", indent, child.label)
		if strings.HasPrefix(child.label, "pre violated:") {
			fmt.Fprintf(sb, "%s\tmustPanic(t, func() {\n", indent)
			fmt.Fprintf(sb, "%s\t})\n", indent)
		} else {
			writeTestTrie(sb, child, depth+1)
		}
		fmt.Fprintf(sb, "%s})\n", indent)
	}
}

const mustPanicHelper = `func mustPanic(t *testing.T, f func()) any {
	t.Helper()
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		f()
	}()
	if recovered == nil {
		t.Fatal("expected panic")
	}
	return recovered
}
`

// =============================================================================
// SCANNER
// =============================================================================

// tokenKind is the raw annotation keyword, including "done" which is a
// structural close-marker consumed entirely by the builder.
type tokenKind int

const (
	tokFun tokenKind = iota
	tokPre
	tokCond
	tokBreak
	tokLoop
	tokDone
	tokReturn
)

type token struct {
	k     tokenKind
	label string
	line  int
}

var (
	annotationRe = regexp.MustCompile(`//\s*talia:(\w+)(?:\(([^)]*)\))?`)

	keywords = map[string]tokenKind{
		"fun":    tokFun,
		"pre":    tokPre,
		"cond":   tokCond,
		"break":  tokBreak,
		"loop":   tokLoop,
		"done":   tokDone,
		"return": tokReturn,
	}
)

// scan opens filename and returns all talia tokens in source order.
func scan(filename string) ([]token, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("talia: open %s: %w", filename, err)
	}
	defer f.Close()

	var tokens []token
	lineNum := 0

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lineNum++

		m := annotationRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}

		kw := strings.ToLower(strings.TrimSpace(m[1]))
		k, ok := keywords[kw]
		if !ok {
			return nil, fmt.Errorf("talia: %s:%d: unknown keyword %q", filename, lineNum, kw)
		}

		tokens = append(tokens, token{
			k:     k,
			label: strings.TrimSpace(m[2]),
			line:  lineNum,
		})
	}

	return tokens, sc.Err()
}

// =============================================================================
// BUILDER
// =============================================================================

// scope tracks one open block (kindLoop or kindCond) on the scope stack.
type scope struct {
	node *node
	k    kind
}

// builder converts a flat token stream into the annotation graph,
// enforcing structural rules inline as each token is consumed.
//
// Rules:
//   - Every annotation must appear inside a function (after talia:fun).
//   - pre must be at function top-level, before any cond/loop/return.
//   - break is only legal when at least one enclosing loop is open.
//   - done requires a matching open scope.
//   - A new fun while scopes are open is an error.
//   - Unclosed scopes at EOF are an error.
type builder struct {
	file      *fileNode
	currentFn *node
	scopes    []scope
	loopDepth int  // number of kindLoop entries in scopes
	seenLogic bool // true once any non-pre node is seen in currentFn
}

func build(file *fileNode, tokens []token) error {
	b := &builder{file: file}
	for _, tok := range tokens {
		if err := b.consume(tok); err != nil {
			return err
		}
	}
	return b.eof()
}

func (b *builder) consume(tok token) error {
	switch tok.k {
	case tokFun:
		return b.onFun(tok)
	case tokPre:
		return b.onPre(tok)
	case tokCond:
		return b.onCond(tok)
	case tokBreak:
		return b.onBreak(tok)
	case tokLoop:
		return b.onLoop(tok)
	case tokDone:
		return b.onDone(tok)
	case tokReturn:
		return b.onReturn(tok)
	}
	return nil
}

func (b *builder) eof() error {
	if b.currentFn != nil && len(b.scopes) > 0 {
		return fmt.Errorf("EOF: function %q has %d unclosed scope(s)",
			b.currentFn.label, len(b.scopes))
	}
	return nil
}

func (b *builder) onFun(tok token) error {
	if b.currentFn != nil && len(b.scopes) > 0 {
		return b.errf(tok,
			"new function %q started while %d scope(s) in %q are still open",
			tok.label, len(b.scopes), b.currentFn.label)
	}
	fn := &node{k: kindFunc, label: tok.label}
	b.file.funcs = append(b.file.funcs, fn)
	b.currentFn = fn
	b.scopes = b.scopes[:0]
	b.loopDepth = 0
	b.seenLogic = false
	return nil
}

func (b *builder) onPre(tok token) error {
	if err := b.requireFn(tok); err != nil {
		return err
	}
	if len(b.scopes) > 0 {
		return b.errf(tok, "pre(%q) must be at function scope, not inside a loop or cond", tok.label)
	}
	if b.seenLogic {
		return b.errf(tok, "pre(%q) must appear before any cond, loop, or return", tok.label)
	}
	b.push(&node{k: kindPre, label: tok.label})
	return nil
}

func (b *builder) onCond(tok token) error {
	if err := b.requireFn(tok); err != nil {
		return err
	}
	b.seenLogic = true
	n := &node{k: kindCond, label: tok.label}
	b.push(n)
	b.scopes = append(b.scopes, scope{node: n, k: kindCond})
	return nil
}

func (b *builder) onBreak(tok token) error {
	if err := b.requireFn(tok); err != nil {
		return err
	}
	if b.loopDepth == 0 {
		return b.errf(tok, "break(%q) outside any loop", tok.label)
	}
	b.seenLogic = true
	b.push(&node{k: kindBreak, label: tok.label})
	return nil
}

func (b *builder) onLoop(tok token) error {
	if err := b.requireFn(tok); err != nil {
		return err
	}
	b.seenLogic = true
	n := &node{k: kindLoop, label: tok.label}
	b.push(n)
	b.scopes = append(b.scopes, scope{node: n, k: kindLoop})
	b.loopDepth++
	return nil
}

func (b *builder) onDone(tok token) error {
	if err := b.requireFn(tok); err != nil {
		return err
	}
	if len(b.scopes) == 0 {
		return b.errf(tok, "done without a matching loop or cond")
	}
	top := b.scopes[len(b.scopes)-1]
	if top.k == kindLoop {
		b.loopDepth--
	}
	b.scopes = b.scopes[:len(b.scopes)-1]
	return nil
}

func (b *builder) onReturn(tok token) error {
	if err := b.requireFn(tok); err != nil {
		return err
	}
	b.seenLogic = true
	b.push(&node{k: kindReturn, label: tok.label})
	return nil
}

// push appends n to the innermost open scope, or to the current function
// when no inner scope is open.
func (b *builder) push(n *node) {
	if len(b.scopes) > 0 {
		top := b.scopes[len(b.scopes)-1]
		top.node.children = append(top.node.children, n)
		return
	}
	b.currentFn.children = append(b.currentFn.children, n)
}

func (b *builder) requireFn(tok token) error {
	if b.currentFn == nil {
		return b.errf(tok, "annotation outside any function — declare talia:fun first")
	}
	return nil
}

func (b *builder) errf(tok token, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", tok.line, fmt.Sprintf(format, args...))
}

// =============================================================================
// PATH ENUMERATION
// =============================================================================

// exitKind classifies how a path left its enclosing sequence.
type exitKind int

const (
	// exitRet: hit a return or pre-violation; function exits.
	exitRet exitKind = iota

	// exitBreak: hit a break; exits the innermost enclosing loop.
	// Consumed and converted to exitFall by walkLoop; never escapes it.
	exitBreak

	// exitFall: reached the natural end of the sequence.
	exitFall
)

type pathResult struct {
	steps []string
	exit  exitKind
}

// paths returns all complete execution paths through fn's body.
func paths(fn *node) [][]string {
	results := walk(fn.children, 0)
	out := make([][]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.steps)
	}
	return out
}

// walk enumerates all typed paths through nodes[i:].
func walk(nodes []*node, i int) []pathResult {
	if i >= len(nodes) {
		return []pathResult{{exit: exitFall}}
	}

	n := nodes[i]

	switch n.k {

	// return: terminal step, function exits.
	case kindReturn:
		return []pathResult{{
			steps: []string{"→ return:" + n.label},
			exit:  exitRet,
		}}

	// pre: violated path is terminal; ok path is a transparent continuation.
	case kindPre:
		violated := pathResult{
			steps: []string{"pre violated: " + n.label},
			exit:  exitRet,
		}
		return append([]pathResult{violated}, walk(nodes, i+1)...)

	// cond: taken branch explores the body; not-taken branch skips it.
	// After either branch, control resumes at nodes[i+1] unless the branch
	// already exited.
	case kindCond:
		takenResults := walk(n.children, 0)
		rest := walk(nodes, i+1)

		var result []pathResult

		for _, tr := range takenResults {
			switch tr.exit {
			case exitRet, exitBreak:
				// Taken branch exits — prepend label and stop.
				result = append(result, pathResult{
					steps: prepend(n.label, tr.steps),
					exit:  tr.exit,
				})
			case exitFall:
				// Taken branch fell through — continue with post-cond siblings.
				takenSteps := prepend(n.label, tr.steps)
				for _, r := range rest {
					result = append(result, pathResult{
						steps: concat(takenSteps, r.steps),
						exit:  r.exit,
					})
				}
			}
		}

		// Not-taken: skip the body, continue directly with rest.
		result = append(result, rest...)

		return result

	// break: taken exits the loop; not-taken continues in the loop body.
	case kindBreak:
		taken := pathResult{steps: []string{n.label}, exit: exitBreak}
		return append([]pathResult{taken}, walk(nodes, i+1)...)

	// loop: expand into symbolic variants, then continue past the loop.
	case kindLoop:
		variants := walkLoop(n)
		rest := walk(nodes, i+1)

		var result []pathResult
		for _, v := range variants {
			switch v.exit {
			case exitRet:
				result = append(result, v)
			case exitFall:
				for _, r := range rest {
					result = append(result, pathResult{
						steps: concat(v.steps, r.steps),
						exit:  r.exit,
					})
				}
			}
		}
		return result
	}

	return nil
}

// walkLoop expands loop into two symbolic variants:
//
//	"label [skipped]"  — body never executes
//	"label"            — body executes (entered)
//
// exitBreak from the body is converted to exitFall: the break terminated
// this loop; callers see the loop as completed normally.
func walkLoop(loop *node) []pathResult {
	body := walk(loop.children, 0)

	prefix := loop.label
	result := make([]pathResult, 0, 1+len(body))

	// Skipped: loop body never executed.
	result = append(result, pathResult{
		steps: []string{prefix + " [skipped]"},
		exit:  exitFall,
	})

	for _, r := range body {
		if r.exit == exitFall {
			continue // pure continuation; not an exit path on its own
		}

		// exitBreak → exitFall: break terminates the loop, not the function.
		exit := r.exit
		if exit == exitBreak {
			exit = exitFall
		}

		result = append(result, pathResult{
			steps: prepend(prefix, r.steps),
			exit:  exit,
		})
	}

	return result
}

// prepend returns a new slice: [label] + rest. rest is never modified.
func prepend(label string, rest []string) []string {
	out := make([]string, 1+len(rest))
	out[0] = label
	copy(out[1:], rest)
	return out
}

// concat returns a new slice: a + b. Neither input is modified.
func concat(a, b []string) []string {
	out := make([]string, len(a)+len(b))
	copy(out, a)
	copy(out[len(a):], b)
	return out
}

// =============================================================================
// PREFIX TRIE — HIERARCHICAL RENDERER
// =============================================================================

// trie is a prefix tree for grouping paths by common step prefixes.
// Insertion order is preserved so output is stable.
type trie struct {
	label    string
	children []*trie
	index    map[string]int // label → index in children (deduplication)
}

func newTrie() *trie {
	return &trie{index: make(map[string]int)}
}

// insert adds a path into the trie, creating child nodes as needed.
func (t *trie) insert(steps []string) {
	if len(steps) == 0 {
		return
	}
	label := steps[0]
	idx, ok := t.index[label]
	if !ok {
		idx = len(t.children)
		t.children = append(t.children, &trie{
			label: label,
			index: make(map[string]int),
		})
		t.index[label] = idx
	}
	t.children[idx].insert(steps[1:])
}

// writeTo renders the subtree as indented bullet lines.
// The root trie has no label and does not consume an indentation level.
func (t *trie) writeTo(sb *strings.Builder, depth int) {
	indent := strings.Repeat("  ", depth)
	if t.label != "" {
		fmt.Fprintf(sb, "%s* %s\n", indent, t.label)
	}
	childDepth := depth
	if t.label != "" {
		childDepth++
	}
	for _, child := range t.children {
		child.writeTo(sb, childDepth)
	}
}

// =============================================================================
// PUBLIC ENTRY POINT
// =============================================================================

// parse reads the Go source file at filename, extracts talia annotations,
// builds the annotation graph, and returns the root fileNode.
func parse(filename string) (*fileNode, error) {
	tokens, err := scan(filename)
	if err != nil {
		return nil, err
	}
	file := &fileNode{name: filename}
	if err := build(file, tokens); err != nil {
		return nil, fmt.Errorf("talia: build %s: %w", filename, err)
	}
	return file, nil
}
