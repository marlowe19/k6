package goja

import (
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"math"
	"reflect"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/unistring"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type unicodeString []uint16

type unicodeRuneReader struct {
	s   unicodeString
	pos int
}

type runeReaderReplace struct {
	wrapped io.RuneReader
}

type unicodeStringBuilder struct {
	buf     []uint16
	unicode bool
}

var (
	InvalidRuneError = errors.New("Invalid rune")
)

func (rr runeReaderReplace) ReadRune() (r rune, size int, err error) {
	r, size, err = rr.wrapped.ReadRune()
	if err == InvalidRuneError {
		err = nil
		r = utf8.RuneError
	}
	return
}

func (rr *unicodeRuneReader) ReadRune() (r rune, size int, err error) {
	if rr.pos < len(rr.s) {
		r = rune(rr.s[rr.pos])
		size++
		rr.pos++
		if isUTF16FirstSurrogate(r) {
			if rr.pos < len(rr.s) {
				second := rune(rr.s[rr.pos])
				if isUTF16SecondSurrogate(second) {
					r = utf16.DecodeRune(r, second)
					size++
					rr.pos++
				} else {
					err = InvalidRuneError
				}
			} else {
				err = InvalidRuneError
			}
		} else if isUTF16SecondSurrogate(r) {
			err = InvalidRuneError
		}
	} else {
		err = io.EOF
	}
	return
}

func (b *unicodeStringBuilder) grow(n int) {
	if cap(b.buf)-len(b.buf) < n {
		buf := make([]uint16, len(b.buf), 2*cap(b.buf)+n)
		copy(buf, b.buf)
		b.buf = buf
	}
}

func (b *unicodeStringBuilder) Grow(n int) {
	b.grow(n + 1)
}

func (b *unicodeStringBuilder) ensureStarted(initialSize int) {
	b.grow(len(b.buf) + initialSize + 1)
	if len(b.buf) == 0 {
		b.buf = append(b.buf, unistring.BOM)
	}
}

func (b *unicodeStringBuilder) writeString(s valueString) {
	b.ensureStarted(int(s.length()))
	switch s := s.(type) {
	case unicodeString:
		b.buf = append(b.buf, s[1:]...)
		b.unicode = true
	case asciiString:
		for i := 0; i < len(s); i++ {
			b.buf = append(b.buf, uint16(s[i]))
		}
	default:
		panic(fmt.Errorf("unsupported string type: %T", s))
	}
}

func (b *unicodeStringBuilder) string() valueString {
	if b.unicode {
		return unicodeString(b.buf)
	}
	if len(b.buf) == 0 {
		return stringEmpty
	}
	buf := make([]byte, 0, len(b.buf)-1)
	for _, c := range b.buf[1:] {
		buf = append(buf, byte(c))
	}
	return asciiString(buf)
}

func (b *unicodeStringBuilder) writeRune(r rune) {
	if r <= 0xFFFF {
		b.ensureStarted(1)
		b.buf = append(b.buf, uint16(r))
		b.unicode = r >= utf8.RuneSelf
	} else {
		b.ensureStarted(2)
		first, second := utf16.EncodeRune(r)
		b.buf = append(b.buf, uint16(first), uint16(second))
		b.unicode = true
	}
}

func (b *unicodeStringBuilder) writeASCII(bytes []byte) {
	b.ensureStarted(len(bytes))
	for _, c := range bytes {
		b.buf = append(b.buf, uint16(c))
	}
}

func (s unicodeString) reader(start int) io.RuneReader {
	return &unicodeRuneReader{
		s: s[start+1:],
	}
}

func (s unicodeString) ToInteger() int64 {
	return 0
}

func (s unicodeString) toString() valueString {
	return s
}

func (s unicodeString) ToString() Value {
	return s
}

func (s unicodeString) ToFloat() float64 {
	return math.NaN()
}

func (s unicodeString) ToBoolean() bool {
	return len(s) > 0
}

func (s unicodeString) toTrimmedUTF8() string {
	if len(s) == 0 {
		return ""
	}
	return strings.Trim(s.String(), parser.WhitespaceChars)
}

func (s unicodeString) ToNumber() Value {
	return asciiString(s.toTrimmedUTF8()).ToNumber()
}

func (s unicodeString) ToObject(r *Runtime) *Object {
	return r._newString(s, r.global.StringPrototype)
}

func (s unicodeString) equals(other unicodeString) bool {
	if len(s) != len(other) {
		return false
	}
	for i, r := range s {
		if r != other[i] {
			return false
		}
	}
	return true
}

func (s unicodeString) SameAs(other Value) bool {
	if otherStr, ok := other.(unicodeString); ok {
		return s.equals(otherStr)
	}

	return false
}

func (s unicodeString) Equals(other Value) bool {
	if s.SameAs(other) {
		return true
	}

	if o, ok := other.(*Object); ok {
		return s.Equals(o.toPrimitive())
	}
	return false
}

func (s unicodeString) StrictEquals(other Value) bool {
	return s.SameAs(other)
}

func (s unicodeString) baseObject(r *Runtime) *Object {
	ss := r.stringSingleton
	ss.value = s
	ss.setLength()
	return ss.val
}

func (s unicodeString) charAt(idx int) rune {
	return rune(s[idx+1])
}

func (s unicodeString) length() int {
	return len(s) - 1
}

func (s unicodeString) concat(other valueString) valueString {
	switch other := other.(type) {
	case unicodeString:
		b := make(unicodeString, len(s)+len(other)-1)
		copy(b, s)
		copy(b[len(s):], other[1:])
		return b
	case asciiString:
		b := make([]uint16, len(s)+len(other))
		copy(b, s)
		b1 := b[len(s):]
		for i := 0; i < len(other); i++ {
			b1[i] = uint16(other[i])
		}
		return unicodeString(b)
	default:
		panic(fmt.Errorf("Unknown string type: %T", other))
	}
}

func (s unicodeString) substring(start, end int) valueString {
	ss := s[start+1 : end+1]
	for _, c := range ss {
		if c >= utf8.RuneSelf {
			b := make(unicodeString, end-start+1)
			b[0] = unistring.BOM
			copy(b[1:], ss)
			return b
		}
	}
	as := make([]byte, end-start)
	for i, c := range ss {
		as[i] = byte(c)
	}
	return asciiString(as)
}

func (s unicodeString) String() string {
	return string(utf16.Decode(s[1:]))
}

func (s unicodeString) compareTo(other valueString) int {
	return strings.Compare(s.String(), other.String())
}

func (s unicodeString) index(substr valueString, start int) int {
	var ss []uint16
	switch substr := substr.(type) {
	case unicodeString:
		ss = substr[1:]
	case asciiString:
		ss = make([]uint16, len(substr))
		for i := 0; i < len(substr); i++ {
			ss[i] = uint16(substr[i])
		}
	default:
		panic(fmt.Errorf("unknown string type: %T", substr))
	}
	s1 := s[1:]
	// TODO: optimise
	end := len(s1) - len(ss)
	for start <= end {
		for i := 0; i < len(ss); i++ {
			if s1[start+i] != ss[i] {
				goto nomatch
			}
		}

		return start
	nomatch:
		start++
	}
	return -1
}

func (s unicodeString) lastIndex(substr valueString, start int) int {
	var ss []uint16
	switch substr := substr.(type) {
	case unicodeString:
		ss = substr[1:]
	case asciiString:
		ss = make([]uint16, len(substr))
		for i := 0; i < len(substr); i++ {
			ss[i] = uint16(substr[i])
		}
	default:
		panic(fmt.Errorf("Unknown string type: %T", substr))
	}

	s1 := s[1:]
	if maxStart := len(s1) - len(ss); start > maxStart {
		start = maxStart
	}
	// TODO: optimise
	for start >= 0 {
		for i := 0; i < len(ss); i++ {
			if s1[start+i] != ss[i] {
				goto nomatch
			}
		}

		return start
	nomatch:
		start--
	}
	return -1
}

func unicodeStringFromRunes(r []rune) unicodeString {
	return unistring.NewFromRunes(r).AsUtf16()
}

func (s unicodeString) toLower() valueString {
	caser := cases.Lower(language.Und)
	r := []rune(caser.String(s.String()))
	// Workaround
	ascii := true
	for i := 0; i < len(r)-1; i++ {
		if (i == 0 || r[i-1] != 0x3b1) && r[i] == 0x345 && r[i+1] == 0x3c2 {
			i++
			r[i] = 0x3c3
		}
		if r[i] >= utf8.RuneSelf {
			ascii = false
		}
	}
	if ascii {
		ascii = r[len(r)-1] < utf8.RuneSelf
	}
	if ascii {
		return asciiString(r)
	}
	return unicodeStringFromRunes(r)
}

func (s unicodeString) toUpper() valueString {
	caser := cases.Upper(language.Und)
	return newStringValue(caser.String(s.String()))
}

func (s unicodeString) Export() interface{} {
	return s.String()
}

func (s unicodeString) ExportType() reflect.Type {
	return reflectTypeString
}

func (s unicodeString) hash(hash *maphash.Hash) uint64 {
	_, _ = hash.WriteString(string(unistring.FromUtf16(s)))
	h := hash.Sum64()
	hash.Reset()
	return h
}

func (s unicodeString) string() unistring.String {
	return unistring.FromUtf16(s)
}
