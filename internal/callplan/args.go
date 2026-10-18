// Package callplan turns a schema-2 instruction name, typed argument values and
// account choices into a complete instruction: argument data encoded with the
// project's resolved wire layout (the same offsets the generated client uses),
// and every declared account resolved from the schema where it can be (fixed
// addresses, PDA seeds, key relations, the default signer) or from explicit
// overrides. `gosvm call` uses it to invoke instructions by name.
package callplan

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"go/types"
	"strconv"
	"strings"
	"unicode"

	"gosvm/internal/network"
	"gosvm/internal/project"
)

// Field is one scalar or scalar-array leaf of an instruction's argument layout.
type Field struct {
	Flag     string // command-line name: kebab-case path, e.g. "amount", "terms.min-out"
	Path     string // Go field path as seeds/relations name it after "args.", e.g. "MinOut"
	Type     string // Go type as declared, e.g. uint64, model.PublicKey
	Offset   int    // absolute offset in the instruction data (after the discriminator)
	Size     int    // total encoded bytes
	Length   int    // array elements; 0 for a scalar
	ElemSize int    // bytes per scalar (the whole Size for a scalar)
	Signed   bool
}

// Fields flattens an argument layout into its leaves in declaration order.
func Fields(l project.WireLayout) []Field {
	var out []Field
	var walk func(fields []project.WireField, flag, path string)
	walk = func(fields []project.WireField, flag, path string) {
		for _, f := range fields {
			fl, p := flag+kebab(f.Name), path+f.Name
			if len(f.Fields) > 0 {
				walk(f.Fields, fl+".", p+".")
				continue
			}
			leaf := Field{Flag: fl, Path: p, Type: shortType(f.GoType, f.Type), Offset: f.Offset, Size: f.Size, ElemSize: f.Size}
			var basic *types.Basic
			if f.GoType != nil {
				switch u := f.GoType.Underlying().(type) {
				case *types.Basic:
					basic = u
				case *types.Array:
					basic, _ = u.Elem().Underlying().(*types.Basic)
				}
			}
			if f.Length > 0 {
				leaf.Length, leaf.ElemSize = f.Length, f.Size/f.Length
			}
			if basic != nil {
				switch basic.Kind() {
				case types.Int8, types.Int16, types.Int32, types.Int64:
					leaf.Signed = true
				}
			}
			out = append(out, leaf)
		}
	}
	walk(l.Fields, "", "")
	return out
}

func shortType(t types.Type, fallback string) string {
	if t == nil {
		return fallback
	}
	s := types.TypeString(t, func(p *types.Package) string { return p.Name() })
	if _, named := t.(*types.Named); named {
		if u := types.TypeString(t.Underlying(), nil); u != s {
			s += " (" + u + ")"
		}
	}
	return s
}

// kebab converts a Go field name to a flag name: MinOut -> min-out, XBump -> x-bump.
func kebab(name string) string {
	var b strings.Builder
	r := []rune(name)
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 && (unicode.IsLower(r[i-1]) || (i+1 < len(r) && unicode.IsLower(r[i+1]))) {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// normal folds a flag or Go path for matching: "min-out", "MinOut" and "minout" agree.
func normal(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}

// Matches reports whether name (without the leading dash) selects this field.
func (f Field) Matches(name string) bool {
	return normal(name) == normal(f.Flag) || normal(name) == normal(f.Path)
}

// Usage is the field's flag with its type, as error messages list it.
func (f Field) Usage() string { return "-" + f.Flag + " " + f.Type }

// Parse encodes a command-line value for this field: integers in decimal or
// 0x hex (negative values for signed types), byte arrays as hex or, for 32
// bytes, a base58 key; other arrays as comma-separated integers.
func (f Field) Parse(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if f.Length == 0 {
		b, err := parseScalar(s, f.ElemSize, f.Signed)
		if err != nil {
			return nil, fmt.Errorf("-%s: %w", f.Flag, err)
		}
		return b, nil
	}
	out := make([]byte, 0, f.Size)
	if f.ElemSize == 1 && !f.Signed && !strings.Contains(s, ",") {
		h := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
		if b, err := hex.DecodeString(h); err == nil && len(b) == f.Length {
			return b, nil
		}
		if f.Length == 32 {
			if b, err := network.DecodeBase58(s); err == nil && len(b) == 32 {
				return b, nil
			}
			return nil, fmt.Errorf("-%s: want a base58 key or 64 hex digits, got %q", f.Flag, s)
		}
		return nil, fmt.Errorf("-%s: want %d bytes as %d hex digits, got %q", f.Flag, f.Length, 2*f.Length, s)
	}
	parts := strings.Split(s, ",")
	if len(parts) != f.Length {
		return nil, fmt.Errorf("-%s: want %d comma-separated values, got %d", f.Flag, f.Length, len(parts))
	}
	for _, p := range parts {
		b, err := parseScalar(strings.TrimSpace(p), f.ElemSize, f.Signed)
		if err != nil {
			return nil, fmt.Errorf("-%s: %w", f.Flag, err)
		}
		out = append(out, b...)
	}
	return out, nil
}

func parseScalar(s string, size int, signed bool) ([]byte, error) {
	var v uint64
	if signed {
		n, err := strconv.ParseInt(s, 0, size*8)
		if err != nil {
			return nil, fmt.Errorf("want an int%d, got %q", size*8, s)
		}
		v = uint64(n)
	} else {
		n, err := strconv.ParseUint(s, 0, size*8)
		if err != nil {
			return nil, fmt.Errorf("want a uint%d, got %q", size*8, s)
		}
		v = n
	}
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b[:size], nil
}

// Format renders encoded field bytes the way Parse accepts them.
func (f Field) Format(b []byte) string {
	if f.Length == 0 {
		return formatScalar(b, f.Signed)
	}
	if f.ElemSize == 1 && !f.Signed {
		if f.Length == 32 {
			return network.Base58(b)
		}
		return hex.EncodeToString(b)
	}
	var parts []string
	for i := 0; i+f.ElemSize <= len(b); i += f.ElemSize {
		parts = append(parts, formatScalar(b[i:i+f.ElemSize], f.Signed))
	}
	return strings.Join(parts, ",")
}

func formatScalar(b []byte, signed bool) string {
	var buf [8]byte
	copy(buf[:], b)
	v := binary.LittleEndian.Uint64(buf[:])
	if signed {
		shift := uint(64 - 8*len(b))
		return strconv.FormatInt(int64(v<<shift)>>shift, 10)
	}
	return strconv.FormatUint(v, 10)
}
