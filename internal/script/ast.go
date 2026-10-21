package script

type pos struct{ line, col int }

type node any // an expression
type stmt any // a statement

type elem struct {
	x      node
	spread bool
}

type (
	constLit struct {
		pos
		v any
	}
	tplPartX struct {
		lit string
		x   node
	}
	tplLit struct {
		pos
		parts []tplPartX
	}
	identX struct {
		pos
		name string
	}
	arrayLit struct {
		pos
		elems []elem
	}
	propX struct {
		key    string
		keyX   node // computed key
		value  node
		spread bool
	}
	objectLit struct {
		pos
		props []propX
	}
	param struct {
		pat pattern
		def node
		// rest is `...name`: the remaining arguments as an array.
		rest bool
	}
	funcLit struct {
		pos
		name   string
		params []param
		body   []stmt
		expr   node // an arrow function with an expression body
	}
	unaryX struct {
		pos
		op string
		x  node
	}
	updateX struct {
		pos
		op     string // ++ or --
		prefix bool
		target node
	}
	binaryX struct {
		pos
		op   string
		l, r node
	}
	logicalX struct {
		pos
		op   string
		l, r node
	}
	condX struct {
		pos
		c, a, b node
	}
	assignX struct {
		pos
		op     string
		target node
		value  node
	}
	callX struct {
		pos
		fn       node
		args     []elem
		optional bool
	}
	memberX struct {
		pos
		obj      node
		name     string
		optional bool
	}
	indexX struct {
		pos
		obj, idx node
		optional bool
	}
)

type (
	identP struct {
		pos
		name string
	}
	patElem struct {
		pat  pattern
		def  node
		skip bool // a hole: [a, , b]
		rest bool
	}
	arrayP struct {
		pos
		elems []patElem
	}
	objProp struct {
		key string
		pat pattern
		def node
	}
	objectP struct {
		pos
		props []objProp
	}
)

type pattern any

type (
	declr struct {
		pat  pattern
		init node
	}
	varDecl struct {
		pos
		konst bool
		decls []declr
	}
	funcDecl struct {
		pos
		name string
		fn   *funcLit
	}
	returnS struct {
		pos
		x node
	}
	ifS struct {
		pos
		c    node
		a, b stmt
	}
	forS struct {
		pos
		init   stmt
		cond   node
		post   node
		body   stmt
		scoped bool
	}
	forOfS struct {
		pos
		konst bool
		pat   pattern
		iter  node
		body  stmt
	}
	whileS struct {
		pos
		c    node
		body stmt
	}
	breakS    struct{ pos }
	continueS struct{ pos }
	blockS    struct {
		pos
		body   []stmt
		scoped bool
	}
	exprS struct {
		pos
		x node
	}
)

func posOf(n any) pos {
	switch v := n.(type) {
	case *constLit:
		return v.pos
	case *tplLit:
		return v.pos
	case *identX:
		return v.pos
	case *arrayLit:
		return v.pos
	case *objectLit:
		return v.pos
	case *funcLit:
		return v.pos
	case *unaryX:
		return v.pos
	case *updateX:
		return v.pos
	case *binaryX:
		return v.pos
	case *logicalX:
		return v.pos
	case *condX:
		return v.pos
	case *assignX:
		return v.pos
	case *callX:
		return v.pos
	case *memberX:
		return v.pos
	case *indexX:
		return v.pos
	case *varDecl:
		return v.pos
	case *funcDecl:
		return v.pos
	case *returnS:
		return v.pos
	case *ifS:
		return v.pos
	case *forS:
		return v.pos
	case *forOfS:
		return v.pos
	case *whileS:
		return v.pos
	case *breakS:
		return v.pos
	case *continueS:
		return v.pos
	case *blockS:
		return v.pos
	case *exprS:
		return v.pos
	case *identP:
		return v.pos
	case *arrayP:
		return v.pos
	case *objectP:
		return v.pos
	}
	return pos{}
}
