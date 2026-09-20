package scene

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// The parser's vocabulary, and what happens to a key outside it.
//
// This is the fifth instance of one defect class and the first one caught at
// the layer where the class actually lives. The previous four were each a
// single field: a style key the validator accepted and styleName() dropped, a
// border token both drawing paths ignored, `row_template` walked by everything
// and drawn by nobody, and then `on_press`/`scroll` — universal in SCENES.md,
// never declared on Node, so encoding/json discarded them without a word.
//
// Fixing the fourth pair by declaring the fields was correct and too narrow.
// encoding/json ignores *every* key it does not recognise, so declaring two of
// them bought exactly two keys. Measured on the tree as it stood after that
// fix, with the whole suite green:
//
//	focus_glow  parsed, validated clean, drew nothing
//	transition  parsed, validated clean, drew nothing
//	reveal      parsed, validated clean, drew nothing
//	enter       parsed, validated clean, drew nothing
//	shine       parsed, validated clean, drew nothing
//	tab         parsed, validated clean, drew nothing
//
// All six are named by SCENES.md — the first four by Scene 4's animation
// paragraph, `shine` by Scene 11, `tab: false` by Q19 — and a seventh probe,
// `totally_invented_key`, behaved identically to all of them. That is the
// tell: the format had no way to distinguish a property it documents from a
// string nobody has ever typed before, because it was not looking at keys at
// all.
//
// The worst case is not the documented gap, though. It is the typo:
//
//	{ "root": { "type": "stack", "chidlren": [ …two nodes… ] } }
//
// parses, validates clean, and renders an empty stack. One transposed pair of
// letters silently deletes the entire subtree, and every layer reports
// success. `{ "scene": { … } }` — the whole tree under the wrong top-level key
// — was also accepted, with a nil root and no complaint.
//
// So the remedy is not another field, it is a vocabulary. nodeVocabulary is
// derived from Node's own json tags by reflection, which is the point: a list
// written by hand would be a second inventory of Node's fields, and this
// package has already watched three hand-maintained inventories drift (the
// signed bind map from BINDS.md in both directions, the unrendered-field map
// from the refusal it advertised, and the universals audit from the engine it
// claimed to check). A field added to Node tomorrow joins the vocabulary for
// free, and one deleted leaves it the same way.
//
// Why a warning rather than a refusal. PLAN.md signs "unknown-but-parseable is
// a warning, missing state is a placeholder", and the engine already honours
// it for node *types*: `button`, `switch`, `slider` and `sparkline` are
// documented, unimplemented, and each draws [[UNKNOWN NODE TYPE]] rather than
// failing the load, so a document written for a later version keeps booting.
// Refusing an unknown *property* would give the format two opposite answers
// for its two kinds of unknown construction, and would break the forward
// compatibility the plan calls a standing risk. A warning closes the asymmetry
// in the other direction: the document still loads, and the author is told
// what the engine did not understand, with an address.
//
// The three outcomes a key may now have are the same three a universal
// property has under the engine audit — honoured, refused with an address, or
// warned about with an address. What no key may do any more is vanish.

// Warning is a non-fatal finding about a document: it loaded, it will render,
// and something in it did not mean what its author probably thought.
//
// It is a distinct type from Error rather than an Error with a severity flag,
// because the two travel different paths and the difference is load-bearing.
// An Error replaces the document with the last good scene (invariant 3); a
// Warning lets the document through. A single type with a flag is a type whose
// callers decide the severity at each call site, and that is how a warning
// becomes silent: one caller forgets to check the flag and the finding is
// gone. The compiler cannot make that mistake with two types.
type Warning struct {
	Loc Loc
	Msg string
	// Form names the nested form this warning is about ("children.array",
	// "prefix.string", …), and is empty on every other warning.
	//
	// It exists because the audit that pins nestedFormReaders has to find
	// *this* warning among a document's warnings, and it used to do so by
	// looking for the branch name in Msg. Measured: that substring is
	// satisfied by the generic unknown-key warning, whose own explanatory
	// text quotes `a misspelled "children" silently drops the whole
	// subtree`. So a document that warned only about a typo would have
	// satisfied the audit's demand that the drop be reported — the guard
	// accepting an unrelated finding as its own evidence, which is the
	// numerator failure this package has now seen four times.
	//
	// Prose is written for the author and is revised whenever the message
	// is improved; an identity is written for the machine reading it. A
	// guard that greps a human sentence is coupled to its wording, and the
	// coupling is invisible until the wording changes underneath it.
	Form string
}

func (w Warning) String() string {
	return fmt.Sprintf("%s: %s", w.Loc.String(), w.Msg)
}

// RefuseEmpty rejects a document that parsed into no tree at all.
//
// It is a refusal where an unknown property is a warning, and the asymmetry is
// deliberate rather than an inconsistency. PLAN.md's forward-compatibility
// rule protects constructions a *later* engine might understand: an unknown
// property could be Scene 4's `reveal` arriving before Phase 4 builds it, so
// the document still loads and the author is told what was skipped. A document
// with no root is not that. There is no version of this format under which a
// treeless scene draws something, so accepting it cannot be forward
// compatibility — it can only hide a mistake, and the mistake it hides is a
// whole interface that vanished.
//
// Measured before it was written: `{ "scene": { … } }` — the entire tree under
// one wrong top-level key — parsed, validated clean, and rendered nothing.
// Refusing it is also what makes invariant 3 do its job, because the raw-scene
// fallback only fires when the load path is told something went wrong.
//
// It is separate from Validate rather than folded into it because Validate is
// called on hand-built documents throughout the suite and in future patch
// code, where a nil root means "nothing to check" rather than "the author lost
// their scene". The load path knows it read bytes off a disk; Validate does
// not.
func (d *Document) RefuseEmpty() error {
	if d == nil {
		return &Error{Msg: "no scene document at all"}
	}
	if d.Root != nil {
		return nil
	}
	return &Error{
		Loc: Loc{File: d.file},
		Msg: "the document declares no \"root\" node, so there is no scene to draw; " +
			"a tree under any other top-level key is discarded by the parser in silence",
	}
}

// nodeVocabulary is every json key Node declares, derived from the struct.
//
// Computed once per call rather than cached in a package var on purpose: the
// cost is a reflect walk over ~20 fields on documents that already cost a JSON
// parse, and a package-level var would be initialised before any test could
// perturb the struct. The injection that proves this guard works needs to be
// able to reason about the vocabulary as a function of Node, not as a snapshot
// taken at init.
func nodeVocabulary() map[string]bool {
	vocab := make(map[string]bool)
	t := reflect.TypeOf(Node{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			vocab[name] = true
		}
	}
	return vocab
}

// Vocabulary returns the json keys a node may declare, sorted.
//
// Exported because the engine's universals audit asks the same question from
// the other side of the boundary, and two packages computing it separately is
// the drift this file exists to stop.
func Vocabulary() []string {
	vocab := nodeVocabulary()
	out := make([]string, 0, len(vocab))
	for key := range vocab {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// borderVocabulary is every json key the border object form declares.
//
// Derived from borderObject — the single type both BorderShape() and
// BorderStyleName() decode into — for the reason this whole file exists:
// those two accessors are what actually reads a border, so they are the only
// honest statement of what a border may contain.
//
// The first version of this function reflected over two anonymous structs
// copied out of the accessors, and an injection showed why that was not
// enough. Teaching BorderStyleName one further key left the copies here
// unchanged, so a document using it rendered correctly and was warned about
// anyway, with the whole suite green: the guard contradicting the renderer,
// in the false-alarm direction, which is the one that gets a guard switched
// off. Reflecting over a copy only relocates the copy. Now there is one type
// and nothing to keep in step.
func borderVocabulary() map[string]bool {
	vocab := make(map[string]bool)
	t := reflect.TypeOf(borderObject{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			vocab[name] = true
		}
	}
	return vocab
}

// styleVocabulary is every key a node's `style` object may name.
//
// It is StyleTokenKeys() — the validator's own list — rather than a copy,
// because that list is already the single statement of "a scene may be written
// this way" and the render path reads it too. A second copy here would let the
// three disagree, which is the exact history StyleTokenKeys' own comment
// records.
// focusGlowVocabulary is derived from FocusGlow, the single type the engine
// reads a glow from. Same contract as borderVocabulary: reflect the type that
// does the reading, never a copy of it.
func focusGlowVocabulary() map[string]bool {
	vocab := make(map[string]bool)
	t := reflect.TypeOf(FocusGlow{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			vocab[name] = true
		}
	}
	return vocab
}

func styleVocabulary() map[string]bool {
	vocab := make(map[string]bool)
	for _, key := range StyleTokenKeys() {
		vocab[key] = true
	}
	return vocab
}

// documentVocabulary is every json key Document declares — in practice `root`,
// derived rather than written for the same reason as the rest.
func documentVocabulary() map[string]bool {
	vocab := make(map[string]bool)
	t := reflect.TypeOf(Document{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			vocab[name] = true
		}
	}
	return vocab
}

// Warnings reports every key in the document that the parser does not
// recognise, addressed to the object that declared it.
//
// It reads the keys recorded by the parse rather than re-deriving them from
// the tree, because by the time there is a tree the evidence is gone: an
// unknown key's whole signature is that it is absent from the parsed Node.
// This is the same reason nodeOffsets is a second pass over the source — the
// token stream is the only place the information exists.
//
// A Document built by hand (no source bytes) has no recorded keys and warns
// about nothing, which is correct: there was no text for a key to be
// misspelled in.
//
// # Why this covers objects that are not nodes
//
// The first version of this guard visited node paths only, and said so on
// purpose: consulting every object in the source would report `style`'s token
// names and `border`'s `shape`/`style` as unknown node properties, a wall of
// false alarms, and a guard that cries wolf is a guard that gets deleted.
//
// That reasoning was right about the danger and wrong about the remedy. It
// treated "has its own vocabulary" as "has no vocabulary", and the difference
// was measured on the tree with the whole suite green:
//
//	{"border":{"shpae":"double"}}       -> parsed, validated clean, drew the
//	                                       default border, warned about nothing
//	{"style":{"tokne":"no_such_token"}} -> parsed, drew unstyled, and the
//	                                       undefined token was never checked
//	{"root":{…},"theme":{…}}            -> accepted in silence
//
// The `style` case is the worst of the three and is new in kind. The other
// silent drops in this project's history lost a property; a misspelled style
// key also *evades the token validator*, because ValidateTokens can only check
// a token it can find. A scene referencing a token that does not exist in the
// theme is supposed to fail the load with an address — two transposed letters
// turn that refusal into a clean bill of health.
//
// And a border typo is invisible in the way that matters least and hurts most:
// `BorderShape()` returns "" for an unrecognised object, "" falls through to
// the default shape, so the box still draws a border — just not the one the
// document asked for. Nothing looks broken.
//
// So the rule is not "warn about node keys" but "every object in a scene
// document has a vocabulary, and a key outside it is reported". Each
// vocabulary is derived from the code that reads that object, never listed
// here.
func (d *Document) Warnings() []Warning {
	if d == nil || d.Root == nil {
		return nil
	}
	var out []Warning

	// The document object itself. Its address is the empty path — the outermost
	// object opens before any key has been read — and a stray key here is how a
	// whole tree disappears under `{"scene": …}`. RefuseEmpty catches that one
	// because it leaves no root at all; this catches its quieter relative, a
	// correct `root` beside a misspelled second copy the author is editing.
	d.warnKeysOf("", documentVocabulary(), &out, func(key string) string {
		return fmt.Sprintf("the document declares %q, which is not a top-level key this engine "+
			"knows; it was ignored. Only %q is read, so a tree written under any other key "+
			"is discarded in silence", key, nodePathRoot)
	})

	d.collectWarnings(d.Root, nodePathRoot, nodeVocabulary(), &out)
	return out
}

// warnKeysOf reports the keys recorded at one object's path that its own
// vocabulary does not contain. The message is a parameter because each object
// needs to say what the author lost, and a single generic sentence
// ("unknown key") would make the cheap cases and the expensive ones read
// alike.
func (d *Document) warnKeysOf(path string, vocab map[string]bool, out *[]Warning, msg func(key string) string) {
	for _, key := range d.declaredKeys[path] {
		if vocab[key] {
			continue
		}
		*out = append(*out, Warning{Loc: d.locOf(path), Msg: msg(key)})
	}
}

// warnDroppedNestedForm reports a nested branch written on an owner that has
// no reader for that shape of it.
//
// The message names the owner, the shape and the owners that *do* read that
// shape, because the mistake this catches is almost always a pairing error
// rather than a typo: the author wrote a construction the format contains,
// against the node that does not compose it. Telling them "unknown property"
// would send them hunting for a misspelling that is not there, and Phase 2's
// repair loop reads these messages — a wrong diagnosis costs a turn the corpus
// then charges to the model.
//
// What was lost is named per branch rather than generically. A dropped prefix
// costs one span; a dropped `children` costs the whole subtree, every node in
// it, and any interface the author built there — the same loss a misspelled
// "children" causes, arriving through a correctly spelled key. Collapsing both
// into "it was ignored" would make the cheap case and the expensive one read
// alike, which is the failure warnKeysOf's per-object messages already exist
// to avoid.
func (d *Document) warnDroppedNestedForm(n *Node, path, branch string, raw []byte, out *[]Warning) {
	if len(raw) == 0 {
		return
	}
	form := nestedFormLabel(branch, raw)
	readers, known := nestedFormReaders[form]
	if !known {
		return
	}
	if _, read := readers[n.Type]; read {
		return
	}

	owners := make([]string, 0, len(readers))
	for owner := range readers {
		owners = append(owners, owner)
	}
	sort.Strings(owners)

	lost := "the span the author wrote is simply absent from the frame"
	if form == "children.array" {
		lost = "the entire subtree the author wrote — every node in it — is simply absent " +
			"from the frame, which is the same loss a misspelled \"children\" causes, " +
			"reached through a key spelled correctly"
	}

	*out = append(*out, Warning{
		Loc:  d.locOf(path),
		Form: form,
		Msg: fmt.Sprintf("node type %q declares %q as %s, and no node type reads that shape "+
			"except %s; it was ignored. The validator still walked it — the binds inside it "+
			"were checked and its tokens were checked against the theme — so the document "+
			"reports success and %s",
			n.Type, branch, nestedShapeNoun(form), strings.Join(owners, " or "), lost),
	})
}

// collectWarnings walks the tree the same way validateBinds does: the walk
// visits node paths, and a node path is the only place the node's own
// vocabulary applies. The sub-objects a node owns are checked here too, each
// against its own vocabulary, because they hang off this node's address and
// nothing else walks them.
func (d *Document) collectWarnings(n *Node, path string, vocab map[string]bool, out *[]Warning) {
	d.warnKeysOf(path, vocab, out, func(key string) string {
		return fmt.Sprintf("node type %q declares %q, which is not a property this engine "+
			"knows; it was ignored. If it is a typo the node lost whatever it named "+
			"(a misspelled \"children\" silently drops the whole subtree); if it is "+
			"from a later version of the format, this engine cannot draw it",
			n.Type, key)
	})

	// Every warning above this line is about a *key* the parser did not
	// recognise. This one is about a *value*, and that gap is precisely how
	// the defect survived: `type` is a perfectly well-known key, so the
	// whole machinery walked past {"type": "buton"} without a word.
	//
	// It is guarded by nodeIsDispatched because not every *Node in the tree
	// is one the renderer looks up by type; see that function for the
	// measured reason.
	if nodeIsDispatched(path) {
		d.warnUnsignedType(n, path, out)
	}

	// The border object, when it is one. A string border ("single") has no keys
	// to check and records no path, so the lookup simply finds nothing.
	d.warnKeysOf(path+".border", borderVocabulary(), out, func(key string) string {
		return fmt.Sprintf("the border of node type %q declares %q, which is not a border "+
			"property this engine knows; it was ignored. A misspelled \"shape\" leaves the "+
			"box drawing this theme's default border rather than the one asked for, which "+
			"looks like nothing went wrong", n.Type, key)
	})

	// The style object. Its keys are the spellings the validator accepts for a
	// token reference, so an unknown one means the token was never read — and
	// therefore never checked against the theme.
	d.warnKeysOf(path+".style", styleVocabulary(), out, func(key string) string {
		return fmt.Sprintf("the style of node type %q declares %q, which is not a key this "+
			"engine reads a token from (%s); it was ignored, so the node draws unstyled and "+
			"the token it names is never checked against the theme — a token that does not "+
			"exist would normally fail the load with an address",
			n.Type, key, strings.Join(StyleTokenKeys(), " or "))
	})

	// The focus_glow object. A misspelled key here is the quiet kind: the
	// glow object survives, so the node still claims the property, and the
	// engine falls back to the ordinary style — the focused row simply never
	// brightens, which reads as focus not working rather than as a typo.
	d.warnKeysOf(path+".focus_glow", focusGlowVocabulary(), out, func(key string) string {
		return fmt.Sprintf("the focus_glow of node type %q declares %q, which is not a "+
			"focus_glow property this engine knows; it was ignored, so the node keeps its "+
			"ordinary style when focused and the glow silently never happens", n.Type, key)
	})

	// The nested positions this node declares, checked against the owners
	// that actually read them. Every warning above this line is about a key
	// or a value the parser did not recognise; this one is about a
	// construction the parser recognised perfectly, validated in full, and
	// handed to an owner with no reader for it — see nestedFormReaders for
	// the four measured rows and why renderNode cannot be the chokepoint
	// here.
	d.warnDroppedNestedForm(n, path, "prefix", n.PrefixRaw, out)
	if n.Suffix != nil {
		d.warnDroppedNestedForm(n, path, "suffix", []byte("{}"), out)
	}
	// `children` is a nested branch by the same argument as the two above,
	// and it was outside this call site until the fifth recurrence: the
	// inventory enumerated the branches the defect had been found in, which
	// is the same mistake one level up from the sweep that enumerated node
	// types. Eleven of the fifteen signed owners drop a child array whole.
	if len(n.Children) > 0 {
		d.warnDroppedNestedForm(n, path, "children", []byte("[]"), out)
	}

	for i, child := range n.Children {
		d.collectWarnings(child, childPath(path, i), vocab, out)
	}
	if prefix := n.PrefixNode(); prefix != nil {
		d.collectWarnings(prefix, prefixPath(path), vocab, out)
	}
	if n.Suffix != nil {
		d.collectWarnings(n.Suffix, suffixPath(path), vocab, out)
	}
	if n.RowTemplate != nil {
		d.collectWarnings(n.RowTemplate, templatePath(path), vocab, out)
	}
}

// NodeVocabularyForAudit exposes the parser's node vocabulary to the audits in
// internal/engine.
//
// It returns the same derived map the warning path uses, and that identity is
// the whole point. The alternative — letting the engine's audit build its own
// view of what Node accepts — is the defect R19h found: a guard that reflected
// over a *copy* of the accessor structs warned about a document the renderer
// honoured, because reflecting a copy only relocates the copy. An audit must
// ask the production vocabulary, or it is measuring its own mirror.
//
// It is a copy of the map rather than the map itself so a caller cannot mutate
// the vocabulary it is asking about. A test that can edit the contract it
// checks is the escape hatch that ends up a blindfold.
func NodeVocabularyForAudit() map[string]bool {
	src := nodeVocabulary()
	out := make(map[string]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// AnimationFieldsForAudit maps the json name of each Scene 4 animation
// property Node declares to its Go field name.
//
// The progress audit needs both halves: the json name is what docs/SCENES.md
// names and what an author writes, the Go field name is what the engine's
// source refers to when it honours one. Deriving the pair here — from the
// single `anim` struct tag on Node's own fields — is what keeps the audit from
// hand-listing the properties it exists to count. A field marked `anim:"1"`
// joins the map by being declared; one deleted leaves it the same way.
//
// The marker is a dedicated tag rather than a name prefix or a list, because
// the two failure modes of the alternatives are known here: a name-based rule
// silently captures an unrelated field added later, and a list is the
// hand-maintained second inventory this package has watched drift four times.
func AnimationFieldsForAudit() map[string]string {
	out := make(map[string]string)
	t := reflect.TypeOf(Node{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get("anim") != "1" {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			out[name] = f.Name
		}
	}
	return out
}
