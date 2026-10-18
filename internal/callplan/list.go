package callplan

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gosvm/internal/network"
	"gosvm/internal/project"
)

// List writes every instruction with its arguments and how each account is
// resolved when not given explicitly.
func List(w io.Writer, s *project.ResolvedSchema) {
	fmt.Fprintf(w, "%s instructions (gosvm call [flags] <Instruction> [-arg value]...; [-arg] is derived when omitted):\n", s.Config.Name)
	for i, ix := range s.Instructions {
		r := &resolver{s: s, ix: ix, in: Inputs{}, fields: Fields(s.Arguments[i])}
		fmt.Fprintf(w, "\n%s", ix.Name)
		for _, f := range r.fields {
			if r.derivable(f) {
				fmt.Fprintf(w, " [-%s %s]", f.Flag, f.Type)
			} else {
				fmt.Fprintf(w, " -%s %s", f.Flag, f.Type)
			}
		}
		fmt.Fprintln(w)
		r.payerAt = r.payerAccount()
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, a := range ix.Accounts {
			how := r.describe(a)
			if k := kindLabel(a); k != "" && a.Kind != "program" {
				how = k + ": " + how
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", a.Name, Privileges(a.Access == "write", isSigner(a)), how)
		}
		tw.Flush()
	}
}

// describe says how an account resolves without explicit flags.
func (r *resolver) describe(d project.AccountDeclaration) string {
	if d.Address != "" {
		k, _ := hex.DecodeString(d.Address)
		if n := WellKnown[network.Base58(k)]; n != "" {
			return n
		}
		return "fixed address " + network.Base58(k)
	}
	if d.PDA != nil {
		return describePDA(*d.PDA, "")
	}
	if isSigner(d) {
		if d.Name == r.payerAt {
			return "the fee payer, unless -signer " + d.Name + "=<keypair file>"
		}
		return "-signer " + d.Name + "=<keypair file>"
	}
	for _, rel := range r.ix.Relations {
		if rel.Account == d.Name {
			if path, ok := strings.CutPrefix(rel.Field, "args."); ok {
				return "from -" + kebabPath(path)
			}
			return "read from " + rel.Field
		}
	}
	if d.Kind == "state" {
		for _, other := range r.s.Instructions {
			for _, o := range other.Accounts {
				if other.Name != r.ix.Name && o.Name == d.Name && o.Layout == d.Layout && o.PDA != nil {
					return describePDA(*o.PDA, other.Name)
				}
			}
		}
	}
	return "-account " + d.Name + "=<address>"
}

func kebabPath(path string) string {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		parts[i] = kebab(p)
	}
	return strings.Join(parts, ".")
}

func describePDA(pda project.PDADeclaration, origin string) string {
	var shown []string
	for _, s := range pda.Seeds {
		switch s.Kind {
		case "bytes":
			b, _ := hex.DecodeString(s.Hex)
			shown = append(shown, showBytes(b))
		case "key":
			shown = append(shown, s.Account)
		default:
			shown = append(shown, s.Field)
		}
	}
	out := "PDA [" + strings.Join(shown, ", ") + "]"
	if pda.Program != "program_id" {
		out += " of " + pda.Program
	}
	if origin != "" {
		out += " as declared in " + origin
	}
	return out
}
