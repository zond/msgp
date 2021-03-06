package gen

import (
	"fmt"
	"strconv"
)

type sizeState uint8

const (
	// need to write "s = ..."
	assign sizeState = iota

	// need to write "s += ..."
	add

	// can just append "+ ..."
	expr
)

type sizeGen struct {
	p     printer
	state sizeState
}

func builtinSize(typ string) string {
	return "msgp." + typ + "Size"
}

// this lets us chain together addition
// operations where possible
func (s *sizeGen) addConstant(sz string) {
	if !s.p.ok() {
		return
	}

	switch s.state {
	case assign:
		s.p.print("\ns = " + sz)
		s.state = expr
		return
	case add:
		s.p.print("\ns += " + sz)
		s.state = expr
		return
	case expr:
		s.p.print(" + " + sz)
		return
	}

	panic("unknown size state")
}

func (s *sizeGen) Execute(p Elem) error {
	if !s.p.ok() {
		return s.p.err
	}
	if !p.Printable() {
		return nil
	}

	s.p.printf("\nfunc (%s %s) Msgsize() (s int) {", p.Varname(), methodReceiver(p, false))
	s.state = assign
	next(s, p)
	s.p.nakedReturn()
	unsetReceiver(p)
	return s.p.err
}

func (s *sizeGen) gStruct(st *Struct) {
	if !s.p.ok() {
		return
	}
	if st.AsTuple {
		s.addConstant(builtinSize(arrayHeader))
		for i := range st.Fields {
			if !s.p.ok() {
				return
			}
			next(s, st.Fields[i].FieldElem)
		}
	} else {
		s.addConstant(builtinSize(mapHeader))
		for i := range st.Fields {
			s.addConstant(builtinSize("StringPrefix"))
			s.addConstant(strconv.Itoa(len(st.Fields[i].FieldTag)))
			next(s, st.Fields[i].FieldElem)
		}
	}
}

func (s *sizeGen) gPtr(p *Ptr) {
	s.state = add // inner must use add
	s.p.printf("\nif %s == nil {\ns += msgp.NilSize\n} else {", p.Varname())
	next(s, p.Value)
	s.state = add // closing block; reset to add
	s.p.closeblock()
}

func (s *sizeGen) gSlice(sl *Slice) {
	if !s.p.ok() {
		return
	}

	s.addConstant(builtinSize(arrayHeader))

	// if the slice's element is a fixed size
	// (e.g. float64, [32]int, etc.), then
	// print the length times the element size directly
	if str, ok := fixedsizeExpr(sl.Els); ok {
		s.addConstant(fmt.Sprintf("(%s * (%s))", lenExpr(sl), str))
		return
	}

	// add inside the range block, and immediately after
	s.state = add
	s.p.rangeBlock(sl.Index, sl.Varname(), s, sl.Els)
	s.state = add
}

func (s *sizeGen) gArray(a *Array) {
	if !s.p.ok() {
		return
	}

	s.addConstant(builtinSize(arrayHeader))

	// if the array's children are a fixed
	// size, we can compile an expression
	// that always represents the array's wire size
	if str, ok := fixedsizeExpr(a); ok {
		s.addConstant(str)
		return
	}

	s.state = add
	s.p.rangeBlock(a.Index, a.Varname(), s, a.Els)
	s.state = add
}

func (s *sizeGen) gMap(m *Map) {
	s.addConstant(builtinSize(mapHeader))
	vn := m.Varname()
	s.p.printf("\nif %s != nil {", vn)
	s.p.printf("\nfor %s, %s := range %s {", m.Keyidx, m.Validx, vn)
	s.p.printf("\n_ = %s", m.Validx) // we may not use the value
	s.p.printf("\ns += msgp.StringPrefixSize + len(%s)", m.Keyidx)
	s.state = expr
	next(s, m.Value)
	s.p.closeblock()
	s.p.closeblock()
	s.state = add
}

func (s *sizeGen) gBase(b *BaseElem) {
	if !s.p.ok() {
		return
	}
	s.addConstant(basesizeExpr(b))
}

// returns "len(slice)"
func lenExpr(sl *Slice) string {
	return "len(" + sl.Varname() + ")"
}

// is a given primitive always the same (max)
// size on the wire?
func fixedSize(p Primitive) bool {
	switch p {
	case Intf, Ext, IDENT, Bytes, String:
		return false
	default:
		return true
	}
}

// strip reference from string
func stripRef(s string) string {
	if s[0] == '&' {
		return s[1:]
	}
	return s
}

// return a fixed-size expression, if possible.
// only possible for *BaseElem and *Array.
// returns (expr, ok)
func fixedsizeExpr(e Elem) (string, bool) {
	switch e := e.(type) {
	case *Array:
		if str, ok := fixedsizeExpr(e.Els); ok {
			return fmt.Sprintf("(%s * (%s))", e.Size, str), true
		}
	case *BaseElem:
		if fixedSize(e.Value) {
			return builtinSize(e.BaseName()), true
		}
	}
	return "", false
}

// print size expression of a variable name
func basesizeExpr(b *BaseElem) string {
	vname := b.Varname()
	if b.Convert {
		vname = tobaseConvert(b)
	}
	switch b.Value {
	case Ext:
		return "msgp.ExtensionPrefixSize + " + stripRef(vname) + ".Len()"
	case Intf:
		return "msgp.GuessSize(" + vname + ")"
	case IDENT:
		return vname + ".Msgsize()"
	case Bytes:
		return "msgp.BytesPrefixSize + len(" + vname + ")"
	case String:
		return "msgp.StringPrefixSize + len(" + vname + ")"
	default:
		return builtinSize(b.BaseName())
	}
}
