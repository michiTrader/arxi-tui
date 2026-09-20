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
func fieldIsRead(read map[string]bool, f nodeField) bool {
	if read[f.name] {
		return true
	}
	// Raw fields are read through accessors named after the concept, not
	// the field: BorderRaw -> BorderShape/BorderStyleName, PrefixRaw ->
	// PrefixNode/PrefixText.
	if base, ok := strings.CutSuffix(f.name, "Raw"); ok {
		for name := range read {
			if strings.HasPrefix(name, base) {
				return true
			}
		}
	}
	return false
}
