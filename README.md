# talia

> You wrote the function. You think you know all the ways it can end.  
> `talia` makes you prove it.

`talia` is a lightweight Go tool that reads structured comments in your source code and **enumerates every distinct execution path** through an annotated function — then turns that list directly into a `testing.T` scaffold with one stub per path. The idea is simple: a function with two loops and three conditionals has more exit combinations than any single `TestFoo` function typically covers. `talia` makes those combinations explicit, names them, and hands you an empty test for each one so nothing gets missed by accident.

No runtime tracing. No AST manipulation. No build tags. Just comments that double as executable documentation, and a tool that holds you to them.

---

## How it works

You annotate the control-flow skeleton of a function with `// talia:` comments:

```go
// talia:fun(insert)
func insert(n *N23, k rune) bool {
    // talia:pre(has parent) → panic
    if hasParent(n) {
        panic("insert into non root node")
    }

    // talia:loop(find target)
    for {
        target = getChild(target, k)
        // talia:cond(target has key)
        if hasKey(target, k) {
            // talia:return(found)
            return true
        } // talia:done
        // talia:break(is leaf)
        if isLeaf(target) {
            break
        }
    } // talia:done(find target)

    addKey(target, k)

    // talia:loop(merge up)
    for {
        if !keyCountEQ(target, 3) {
            // talia:break(no overflow)
            break
        }
        // ... split and rebalance
    } // talia:done(merge up)

    return false // talia:return(not found)
}
```

Run `talia` against the file:

```
$ go run . insert.go
insert.go
* insert
  * pre violated: has parent
  * find target [skipped]
    * merge up [skipped]
      * → return:not found
    * merge up
      * no overflow
        * → return:not found
  * find target
    * target has key
      * → return:found
    * is leaf
      * merge up [skipped]
        * → return:not found
      * merge up
        * no overflow
          * → return:not found
```

Each leaf in that tree is a complete execution path. The tree makes shared prefixes visible so you can immediately see which branches are siblings, which are exclusive, and which paths you hadn't considered.

---

## Generating a test scaffold

`talia` can also emit a ready-to-compile Go test file with one `t.Run` stub per path:

```
$ go run . insert.go
```

The output mirrors the path tree as nested `t.Run` calls:

```go
func TestInsert(t *testing.T) {
    t.Run("pre violated: has parent", func(t *testing.T) {
        mustPanic(t, func() {
            // TODO
        })
    })
    t.Run("find target [skipped]", func(t *testing.T) {
        t.Run("merge up [skipped]", func(t *testing.T) {
            t.Run("→ return:not found", func(t *testing.T) {
            })
        })
        t.Run("merge up", func(t *testing.T) {
            t.Run("no overflow", func(t *testing.T) {
                t.Run("→ return:not found", func(t *testing.T) {
                })
            })
        })
    })
    t.Run("find target", func(t *testing.T) {
        t.Run("target has key", func(t *testing.T) {
            t.Run("→ return:found", func(t *testing.T) {
            })
        })
        t.Run("is leaf", func(t *testing.T) {
            // ...
        })
    })
}
```

The file compiles and passes green immediately — empty stubs are valid tests. You fill in assertions path by path, at your own pace, with a clear record of what's still missing.

`pre violated:` paths (panic guards) get a `mustPanic` body automatically, since that's the only sensible assertion for them:

```go
t.Run("pre violated: has parent", func(t *testing.T) {
    mustPanic(t, func() {
        // TODO: call insert with a non-root node
    })
})
```

The `mustPanic` helper is appended to the generated file:

```go
func mustPanic(t *testing.T, f func()) any {
    t.Helper()
    var recovered any
    func() {
        defer func() { recovered = recover() }()
        f()
    }()
    if recovered == nil {
        t.Fatal("expected panic")
    }
    return recovered
}
```

---

## Annotation reference

| Annotation | Meaning | Structural rules |
|---|---|---|
| `talia:fun(name)` | Opens a function scope | Must precede all other annotations for that function |
| `talia:pre(label)` | Panic guard / precondition | Function top-level only; must come before any `cond`, `loop`, or `return` |
| `talia:loop(label)` | Opens a loop scope | Closed by `talia:done` |
| `talia:break(label)` | Conditional exit from a loop | Legal only inside an enclosing `loop` scope |
| `talia:cond(label)` | Opens a conditional scope | Closed by `talia:done` |
| `talia:done` | Closes the nearest open `loop` or `cond` | Must have a matching open scope |
| `talia:return(label)` | Explicit function exit | Legal anywhere inside a function |

**`talia:pre` vs `talia:cond`:** a `pre` is a guard that terminates the function if violated — it contributes exactly one *extra* path (the violation) and is otherwise transparent to the paths below it. A `cond` contributes two paths: taken and not-taken.

**`talia:break` vs `talia:return`:** a `break` exits the enclosing loop; the function continues. A `return` exits the function entirely. The distinction matters for path enumeration — a `break` inside a loop produces paths that continue past the loop, while a `return` terminates them.

**`talia:loop` semantics:** every loop produces two symbolic variants — `[skipped]` (body never executes) and entered (body executes at least once). Paths that purely fall through the body without hitting a `break` or `return` are not themselves exit paths; they represent the loop iterating, not completing.

---

## Validation

`talia` validates annotation structure while parsing and reports errors with line numbers before producing any output:

```
line 42: break("no overflow") outside any loop
line 67: pre("validate input") must appear before any cond, loop, or return
line 89: EOF: function "process" has 1 unclosed scope(s)
```

This means annotations are self-documenting *and* self-checking. A misplaced annotation is a build error, not a silently wrong path tree.

---

## Installation

```bash
git clone https://github.com/you/talia
cd talia
go build -o talia .
```

Or run directly without installing:

```bash
go run . <source.go>
```

Requires Go 1.21 or later. No dependencies outside the standard library.

---

## Limitations worth knowing

**Annotations are not coupled to the AST.** `talia` reads comments; the Go compiler does not. If you restructure a function without updating the annotations, the path tree will silently describe code that no longer exists. Treat annotation updates the same way you treat keeping comments current — it's a discipline, not a guarantee.

**Loops model two symbolic variants, not full iteration.** `talia` does not unroll loops or reason about loop-carried state. The `[skipped]` and entered variants cover the two structurally interesting cases — does this code reach the body or not — which is what matters for test coverage. Multi-iteration behaviour is outside scope.

**One annotation per line.** The scanner matches the first `talia:` token on each line and ignores the rest. Put each annotation on its own line.

---

## Philosophy

Testing tools often work at the wrong level of abstraction. Coverage tools tell you which lines were hit; they say nothing about whether the combinations of branches that matter were exercised. Formal verification tools are rigorous but require significant investment. `talia` sits in between: it asks you to name the logical structure of your function once, and in return it gives you a complete, human-readable inventory of what needs testing.

The annotations are also useful without the tool. Reading a function annotated with `talia` comments gives you an immediate structural summary — the loops, the guards, the exit points — that prose comments rarely provide as clearly. The tool just makes that structure computable.
