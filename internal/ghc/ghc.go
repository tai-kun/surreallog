package ghc

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode"
	"unicode/utf8"
)

var (
	ErrSyntax     = errors.New("invalid syntax")
	errIgnore     = errors.New("ignore")
	ErrNoParam    = errors.New("no param")
	ErrOutOfRange = errors.New("out of range")
)

func stringOption(s []byte) (any, error) {
	if s == nil {
		return "", ErrNoParam
	}

	return string(s), nil
}

func naturalNumOption(s []byte) (any, error) {
	if s == nil {
		return 0, ErrNoParam
	}

	var num int64
	if err := json.Unmarshal(s, &num); err != nil {
		return 0, err
	}
	if num < 1 {
		return 0, ErrOutOfRange
	}

	return num, nil
}

type GHCOptions struct {
	data map[string][]byte
	defs map[string]func(s []byte) (any, error)
}

// func (o *GHCOptions) RequiredString(p string) {
// 	o.defs[p] = stringOption
// }

// func (o *GHCOptions) RequiredNaturalNum(p string) {
// 	o.defs[p] = naturalNumOption
// }

func (o *GHCOptions) String(p string) {
	o.defs[p] = func(s []byte) (any, error) {
		if s == nil {
			return "", errIgnore
		}

		return stringOption(s)
	}
}

func (o *GHCOptions) NaturalNum(p string) {
	o.defs[p] = func(s []byte) (any, error) {
		if s == nil {
			return "", errIgnore
		}

		return naturalNumOption(s)
	}
}

func (o *GHCOptions) StringWithDefault(p string, v string) {
	o.defs[p] = func(s []byte) (any, error) {
		if s == nil {
			return v, nil
		}

		return stringOption(s)
	}
}

func (o *GHCOptions) NaturalNumWithDefault(p string, v int64) {
	o.defs[p] = func(s []byte) (any, error) {
		if s == nil {
			return v, nil
		}

		return naturalNumOption(s)
	}
}

func (o *GHCOptions) Map() (map[string]any, error) {
	out := map[string]any{}
	for p, into := range o.defs {
		var v any
		var err error
		if s, ok := o.data[p]; ok {
			v, err = into(s)
		} else {
			v, err = into(nil)
		}
		if err != nil {
			if err != errIgnore {
				return nil, err
			}
		} else {
			out[p] = v
		}
	}

	return out, nil
}

type GHC struct {
	Name string
	Data []byte
	Opts *GHCOptions
}

func (g *GHC) OmitData() {
	g.Data = []byte{}
}

func (g *GHC) OmitOpts() {
	g.Opts = nil
}

func (g *GHC) NameOnly() {
	g.OmitData()
	g.OmitOpts()
}

// https://github.com/actions/toolkit/blob/f0b00fd201c7ddf14e1572a10d5fb4577c4bd6a2/packages/core/src/command.ts
func PraseGHC(s []byte) (*GHC, error) {
	s = TrimLeftSpace(s)
	if len(s) <= 4 || s[0] != ':' || s[1] != ':' {
		return nil, ErrSyntax
	}

	ghc := &GHC{
		Name: "",
		Data: []byte{},
		Opts: nil,
	}
	opt := map[string][]byte{}
	ok := false
a:
	for i := 2; i < len(s); i++ {
		switch s[i] {
		case ':':
			if i == len(s)-1 || s[i+1] != ':' {
				return nil, ErrSyntax
			}
			ghc.Name = string(s[2:i])
			msg := s[i+2:]
			ghc.Data = unescapeData(msg)
			ok = true
			break a

		case ' ':
			ghc.Name = string(s[2:i])
			for j, k, val, key := i+1, i+1, false, ""; j < len(s); j++ {
				switch s[j] {
				case '=':
					if val {
						return nil, ErrSyntax
					}
					key = string(s[k:j])
					val = true
					k = j + 1

				case ',':
					if !val {
						return nil, ErrSyntax
					}
					if prop := unescapeProperty(s[k:j]); prop != nil {
						opt[key] = prop
					}
					key = ""
					val = false
					k = j + 1

				case ':':
					if j == len(s)-1 || s[j+1] != ':' {
						return nil, ErrSyntax
					}
					if val {
						if prop := unescapeProperty(s[k:j]); prop != nil {
							opt[key] = prop
						}
					}
					msg := s[j+2:]
					ghc.Data = unescapeData(msg)
					ok = true
					break a
				}
			}
		}
	}

	if ok {
		ghc.Opts = &GHCOptions{
			data: opt,
			defs: map[string]func(s []byte) (any, error){},
		}
		return ghc, nil
	}

	return nil, ErrSyntax
}

var asciiSpace = [256]uint8{'\t': 1, '\n': 1, '\v': 1, '\f': 1, '\r': 1, ' ': 1}

func TrimLeftSpace(s []byte) []byte {
	start := 0
	for ; start < len(s); start++ {
		c := s[start]
		if c >= utf8.RuneSelf {
			return bytes.TrimFunc(s[start:], unicode.IsSpace)
		}
		if asciiSpace[c] == 0 {
			break
		}
	}

	return s[start:]
}

func unescapeData(s []byte) []byte {
	if len(s) == 0 {
		return []byte{}
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i < len(s)-2 {
			switch s[i+1] {
			case '0':
				switch s[i+2] {
				case 'A', 'a':
					s = unescape(s, i, '\n')
				case 'D', 'd':
					s = unescape(s, i, '\r')
				}
			case '2':
				switch s[i+2] {
				case '5':
					s = unescape(s, i, '%')
				}
			}
		}
	}

	return s
}

func unescapeProperty(s []byte) []byte {
	if len(s) == 0 {
		return nil
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i < len(s)-2 {
			switch s[i+1] {
			case '0':
				switch s[i+2] {
				case 'A', 'a':
					s = unescape(s, i, '\n')
				case 'D', 'd':
					s = unescape(s, i, '\r')
				}
			case '2':
				switch s[i+2] {
				case '5':
					s = unescape(s, i, '%')
				case 'C', 'c':
					s = unescape(s, i, ',')
				}
			case '3':
				switch s[i+2] {
				case 'A', 'a':
					s = unescape(s, i, ':')
				}
			}
		}
	}

	return s
}

func unescape(s []byte, i int, c byte) []byte {
	s = append(s[:i+1], s[i+3:]...)
	s[i] = c
	return s
}
