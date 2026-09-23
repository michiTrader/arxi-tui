package scene

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// The structural audit for one defect class: a field the format declares, the
// validator understands, and the renderer never reads.
//
// Three instances have now been paid for, each found by hand and each costing
// a session:
//
//  1. a style key ValidateTokens accepted that styleName() dropped — the scene
//     validated clean and drew unstyled;
//  2. a border token the validator checked and both drawing paths ignored —
//     naming a *bad* token got a refusal, so the field looked wired;
//  3. `row_template`, walked by the validator, addressed by loc.go, collected
//     by the binds audit and by eval's CollectBinds, drawn by nobody — which
//     let a corpus answer score converged on a panel rendering "[…]".
//
// All three share a signature and a symptom. The signature: the field is
// *checked*, which is exactly what makes it look connected. The symptom: the
// document reports success and the screen is wrong, so there is no diagnostic
// anywhere. Of the three possible outcomes for a mis-wired field — refusal,
// correct render, silent drop — the silent drop is the worst, and it is the
// one this class produces by construction.
//
// Finding the fourth by hand is not a plan. This test enumerates the fields of
// Node from the source and asks, of each, whether anything under internal/
// outside the scene package reads it. A field nobody reads is either a known
// gap (declared in unrenderedFields, so the validator refuses it and the
// author gets an addressed reason) or an accepted no-op that must be justified
// here in writing. What it may not be is unnoticed.
//
// This is deliberately a source-level audit rather than a behavioural one. A
// behavioural test would need a scene per field and would only cover the
// fields someone remembered to write a scene for — which is the same
// hand-enumeration that missed these three.
func TestEveryNodeFieldIsEitherRenderedRefusedOrJustified(t *testing.T) {
	fields := nodeFieldsFromSource(t)
	if len(fields) < 15 {
		t.Fatalf("parsed only %d fields off Node; the audit is reading the wrong type and would pass vacuously", len(fields))
	}

	readers := selectorsOnNode(t)

	for _, f := range fields {
		t.Run(f.name, func(t *testing.T) {
			if reason, ok := acceptedUnreadFields[f.name]; ok {
				// Justified no-ops still have to be true: if the engine has
				// since learned to read one, the justification is stale and
				// saying so out loud is the point of the entry.
				if fieldIsRead(readers, f) {
					t.Errorf("%q is listed in acceptedUnreadFields (%q) but something now reads it;\n"+
						"the list has become a comment that is false. Remove the entry.", f.name, reason)
				}
				return
			}

			if _, refused := unrenderedFields[f.jsonName]; refused {
				return // the validator refuses it; the author gets an address
			}

			if !fieldIsRead(readers, f) {
				t.Errorf("Node.%s (json %q) is declared by the format and read by nothing outside internal/scene.\n"+
					"consequence: a scene may set it, pass validation, and have it silently dropped — the outcome that\n"+
					"reports success and shows the wrong screen, with no diagnostic anywhere. This has happened three\n"+
					"times already (style key, border token, row_template).\n"+
					"remedy: render it; or add it to scene.unrenderedFields so the validator refuses it with an address;\n"+
					"or, if it is genuinely inert, add it to acceptedUnreadFields with the reason in writing.",
					f.name, f.jsonName)
			}
		})
	}
}

// acceptedUnreadFields are fields nothing outside this package reads, and that
// is correct. Each entry is a claim that the field's absence from the render
// path costs the user nothing — and each had to be measured, not assumed.
var acceptedUnreadFields = map[string]string{
	// Measured: the menu filters correctly with this field absent, because
	// filtering is the host's job (fold.FilterSlashMatches) and the scene
	// only names the list. Rendering with filter_by:"typed" and with a
	// nonsense value produces byte-identical frames, and removing it
	// entirely still filters. So the field is decorative rather than
	// silently dropped: no behaviour is lost by ignoring it.
	//
	// It stays accepted rather than refused because SOBRIA — the shipped
	// default interface — declares it, and SCENES.md Scene 2 writes it. A
	// refusal would break the factory scene to enforce a rule the factory
	// scene itself predates. When list filtering becomes scene-directed
	// (more than one filter source), this entry is what should fail.
	"FilterBy": "the host always filters; the field names a behaviour that happens without it",

	// Measured: declared tabs never reach the screen — a list declaring a
	// category nobody else uses renders no tab for it. Unlike FilterBy this
	// one IS a silent drop, and unlike row_template it cannot be refused:
	// SOBRIA ships eleven of them, so refusing would break the default
	// interface at boot, which invariant 1 forbids more strongly than this
	// audit demands.
	//
	// The honest state is therefore: known gap, no refusal available,
	// recorded here so it is not rediscovered as a surprise. Category tabs
	// are Scene 5 / Phase 3 work alongside row_template; when they land,
	// this entry fails and that is the signal to delete it.
	"Categories": "declared by SOBRIA and not yet drawn; refusing would break the factory scene (invariant 1)",
}

type nodeField struct {
	name     string // Go field name, e.g. "RowTemplate"
	jsonName string // json tag name, e.g. "row_template"
}

// nodeFieldsFromSource reads the Node declaration out of node.go rather than
// using reflection. Reflection would give the same names, but this test exists
// to catch a *newly added* field that nobody wired up, and reading the source
// keeps the audit anchored to the declaration a contributor actually edits.
func nodeFieldsFromSource(t *testing.T) []nodeField {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "node.go", nil, 0)
	if err != nil {
		t.Fatalf("parse node.go: %v", err)
	}

	var out []nodeField
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Node" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, f := range st.Fields.List {
			for _, name := range f.Names {
				if !name.IsExported() {
					continue
				}
				out = append(out, nodeField{name: name.Name, jsonName: jsonTagName(f.Tag)})
			}
		}
		return false
	})
	return out
}

var jsonTagPattern = regexp.MustCompile(`json:"([^",]+)`)

func jsonTagName(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	if m := jsonTagPattern.FindStringSubmatch(tag.Value); m != nil {
		return m[1]
	}
	return ""
}

// selectorsOnNode returns the set of field and method names that code outside
// this package selects on a value whose type really is scene.Node. The types
// are resolved with go/types rather than matched as text.
//
// The text version of this check is what a first draft reaches for, and it was
// wrong in both directions on its first run. It reported Node.ID as read
// because internal/eval's Case type also has an ID and corpus.go writes c.ID a
// dozen times — a false positive on the one field the audit was meant to
// interrogate. A guard that cries wolf is a guard that gets deleted, and this
// one exists precisely because three real instances slipped past human
// reading; it has to be worth trusting on the fourth.
//
// This package's own files are excluded on purpose. Validating a field is
// exactly what makes an unrendered field look connected — it is the shared
// signature of all three instances — so counting scene's own reads would make
// the audit agree with the bug. That is the mistake the token tests made:
// they asserted through the validator's key rather than the scenes', so code
// and tests shared one wrong assumption and confirmed each other.
func selectorsOnNode(t *testing.T) map[string]bool {
	t.Helper()

	pkgDirs := goPackageDirs(t)
	if len(pkgDirs) == 0 {
		t.Fatal("found no packages outside internal/scene; the audit would pass vacuously")
	}

	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	conf := types.Config{
		Importer: imp,
		// The audit reads types, and a package that fails to type-check
		// for an unrelated reason must not silently report every field as
		// unread. Errors are collected per package and judged below.
		Error: func(error) {},
	}

	found := make(map[string]bool)
	checked := 0

	for _, dir := range pkgDirs {
		files, err := parseDir(fset, dir)
		if err != nil || len(files) == 0 {
			continue
		}
		info := &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Uses:       make(map[*ast.Ident]types.Object),
		}
		// Type-check errors are tolerated: partial information still
		// resolves most selectors, and a package that will not check at
		// all is caught by the `checked` floor below.
		_, _ = conf.Check(packagePathFor(dir), fset, files, info)
		if len(info.Selections) == 0 {
			continue
		}
		checked++

		for sel, selection := range info.Selections {
			if isSceneNode(selection.Recv()) {
				found[sel.Sel.Name] = true
			}
		}
	}

	if checked == 0 {
		t.Fatal("type-checked no package outside internal/scene; the audit cannot tell a\n" +
			"dead field from a build problem, and reporting every field as unread would\n" +
			"be a wall of false alarms. Fix the build before trusting this test.")
	}
	return found
}

// isSceneNode reports whether a receiver type is scene.Node, through any
// number of pointers. Nodes are passed as *scene.Node nearly everywhere.
func isSceneNode(t types.Type) bool {
	for {
		ptr, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Node" &&
		obj.Pkg() != nil && strings.HasSuffix(obj.Pkg().Path(), "internal/scene")
}

// goPackageDirs lists every directory holding non-test Go files under the
// module, except this package's own.
func goPackageDirs(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	seen := make(map[string]bool)
	var dirs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "docs":
				return filepath.SkipDir
			}
			if strings.HasSuffix(path, filepath.Join("internal", "scene")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if dir := filepath.Dir(path); !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return dirs
}

func parseDir(fset *token.FileSet, dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			return nil, perr
		}
		files = append(files, f)
	}
	return files, nil
}

// packagePathFor turns a relative directory into the module-qualified import
// path the type checker records for it.
func packagePathFor(dir string) string {
	clean := filepath.ToSlash(filepath.Clean(dir))
	clean = strings.TrimPrefix(clean, "../../")
	return "github.com/michiTrader/arxi_tui/" + clean
}

// fieldIsRead reports whether anything outside this package reads the field,
// directly (n.RowTemplate) or through an accessor this package exports for it
// (n.BorderStyleName()). The accessor case matters: BorderRaw and PrefixRaw
// are never selected by name, and a check that only looked for the field
// itself would have called two correctly-wired fields dead.
//
// # Why the accessors are derived rather than guessed from the field's name
//
// This resolved a field to its accessors by *spelling*: strip a `Raw` suffix
// and accept any read method whose name starts with what is left. Both halves
// of that are a guess about naming, and the guess is wrong in both directions
// — which is the shape the sibling audit in this package has now had fixed
// eight times, and the shape this file still had. Measured, each number from a
// run rather than an argument:
//
//	TitleRaw json.RawMessage, decoded by nobody        -> this audit green
//	  because CutSuffix yields "Title" and the engine reads n.Title, so an
//	  unrelated field's name vouched for it. The sibling audit caught it on
//	  the raw-classification floor — and then, having followed the remedy
//	  that failure prints (classify it in rawBranchAccessors as carrying no
//	  node), the whole suite went green again with the field still dead:
//	  {"type":"text","title_raw":{…,"bind":"totally.invented"}}
//	    -> validates clean, the unsigned bind is never refused, never drawn
//
//	Caption json.RawMessage, genuinely read by the engine through
//	CaptionGlyph()                                     -> falsely accused
//	  the false-alarm direction, and the one that gets an audit deleted: a
//	  correctly wired field whose accessor is named after the concept rather
//	  than after the field. `Scroll` was the standing example of a raw field
//	  with no `Raw` suffix that escaped notice only because unrenderedFields
//	  refused it; it graduated (G2) to a read struct the engine draws on a
//	  marquee, so it now clears this audit by being read, not by being refused.
//
// So the accessors are read out of the method bodies: a field is read when
// something outside selects it, or when it selects a method of Node whose body
// reaches that field. That is the same derivation `accessorFields` makes in
// nested_branch_audit_test.go, pointed the other way — there, method to the
// field it returns; here, field to the methods that read it.
//
// This does not weaken what the audit means by "read". Selecting `n.Title`
// directly has never proved the value reaches a frame either; the claim is
// about the field being *connected to something outside this package*, and an
// accessor is exactly that connection.
//
// # Proved in both directions, by counterfactuals actually run
//
//	TitleRaw, decoded by nobody, before        -> green (the silent drop)
//	the same, after                            -> caught
//	Caption read through CaptionGlyph(), before -> falsely accused
//	the same, after                            -> accepted
//	an accessor named for the field that reads
//	  something else (`return n.Title`)        -> still caught
//	Caption reachable only two hops out,
//	  with the fixpoint disabled               -> falsely accused
//
// The third line is what keeps the resolution from becoming a hiding place:
// naming a method after a field must not launder it, only reading it counts.
// The fourth isolates the call chain, and it is the one that needed isolating
// — see the negative finding below.
//
// # A negative finding, recorded because it was nearly reported as a pass
//
// Disabling the fixpoint on the clean tree leaves this audit green, so the
// chaining is not load-bearing *today*. Measured rather than assumed: the
// methods reaching BorderRaw are BorderShape, BorderStyleName, HasBorder,
// border and declaredUnrenderedFields, and three of those are selected from
// outside. BorderStyleName reaches the field only through border(), but
// BorderShape and HasBorder read it directly, so a one-hop sibling already
// covers it. The chain earns its place on the shape, not on today's tree:
// remove the two direct readers and BorderStyleName is the only path left.
// Stating this here rather than deleting the fixpoint, because the alternative
// is a guard that is correct by luck and fails the first time an accessor is
// refactored into two.
func fieldIsRead(read map[string]bool, f nodeField) bool {
	if read[f.name] {
		return true
	}
	for method := range read {
		if fieldsReadByAccessors()[method][f.name] {
			return true
		}
	}
	return false
}

// fieldsReadByAccessors maps each method of Node to the fields its body
// reaches, following calls to other methods of Node.
//
// # Why the call chain is followed
//
// BorderStyleName() selects no field at all: it calls the unexported border(),
// which is what reads BorderRaw. A one-level version would report BorderRaw as
// read by nobody — a false alarm on the field whose accessor motivated this
// whole branch of the check. Chaining is not a generalisation for its own
// sake; it is the shape the two accessors in node.go already have.
//
// # Why go/types rather than the spelling of the receiver
//
// The standing rule of this package, paid for five times: when a guard can ask
// the type checker, matching an identifier name is a different question. A
// body selecting `c.Style` on some other struct is not Node.Style, and a
// method on a local type someone names Node later is not a method on this one.
var fieldsReadByAccessors = func() func() map[string]map[string]bool {
	var once sync.Once
	var result map[string]map[string]bool
	return func() map[string]map[string]bool {
		once.Do(func() { result = computeAccessorFieldReads() })
		return result
	}
}()

func computeAccessorFieldReads() map[string]map[string]bool {
	out := make(map[string]map[string]bool)

	fset := token.NewFileSet()
	files, err := parseDir(fset, ".")
	if err != nil || len(files) == 0 {
		return out
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{
		Importer: importer.ForCompiler(fset, "source", nil),
		Error:    func(error) {},
	}
	// The import path must end in internal/scene or isSceneNode rejects
	// this package's own Node — the mistake documentWalkers made on its
	// first run, where packagePathFor(".") yielded a path ending in "/.".
	_, _ = conf.Check("github.com/michiTrader/arxi_tui/internal/scene", fset, files, info)

	// What each method selects on a Node, split into fields of Node and
	// calls to other methods of Node.
	type methodBody struct {
		fields  map[string]bool
		methods map[string]bool
	}
	bodies := make(map[string]*methodBody)

	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			tv, ok := info.Types[fn.Recv.List[0].Type]
			if !ok || !isSceneNode(tv.Type) {
				continue
			}
			mb := &methodBody{fields: make(map[string]bool), methods: make(map[string]bool)}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				selection, ok := info.Selections[sel]
				if !ok || !isSceneNode(selection.Recv()) {
					return true
				}
				if selection.Kind() == types.MethodVal {
					mb.methods[sel.Sel.Name] = true
				} else {
					mb.fields[sel.Sel.Name] = true
				}
				return true
			})
			bodies[fn.Name.Name] = mb
		}
	}

	// Fixpoint over the call chain, so BorderStyleName inherits what
	// border() reads. Bounded by the method count: each pass either adds a
	// field somewhere or the closure is complete.
	for range bodies {
		changed := false
		for _, mb := range bodies {
			for callee := range mb.methods {
				cb, ok := bodies[callee]
				if !ok {
					continue
				}
				for field := range cb.fields {
					if !mb.fields[field] {
						mb.fields[field] = true
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}

	for name, mb := range bodies {
		out[name] = mb.fields
	}
	return out
}
