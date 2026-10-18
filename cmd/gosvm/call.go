package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"gosvm/internal/callplan"
	"gosvm/internal/network"
	"gosvm/internal/project"
)

const callUsage = `usage:
  gosvm call -network N [flags] <Instruction> [-arg value]...   call a schema-2 instruction by name
  gosvm call -list                                             list instructions, arguments and accounts
  gosvm call -network N [flags] -data <hex> [-account ...]     raw instruction data
`

// accountFlag collects repeated -account/-signer values in command-line order.
type accountFlag struct {
	entries *[]accountEntry
	signer  bool
}

type accountEntry struct {
	value  string
	signer bool
}

func (a accountFlag) String() string { return "" }
func (a accountFlag) Set(v string) error {
	*a.entries = append(*a.entries, accountEntry{v, a.signer})
	return nil
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// callCommand invokes a deployed program: a schema-2 instruction by name, with
// typed arguments and accounts resolved from the schema, or raw hex data. It
// simulates by default; -send commits a transaction; -dry-run only resolves.
func callCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm call", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), callUsage+"\nflags:\n")
		fs.PrintDefaults()
	}
	name := fs.String("network", "", "network the program was deployed to")
	dir := fs.String("dir", ".", "project directory (for the schema and deploy record)")
	programID := fs.String("program", "", "program ID (default: from deploy/<network>.json)")
	data := fs.String("data", "", "raw instruction data, hex (instead of an instruction name)")
	payer := fs.String("keypair", "", "fee payer keypair, also the default signer (default: the project's deploy/keys/<network>-payer.json)")
	url := fs.String("url", "", "override the network's RPC URL")
	send := fs.Bool("send", false, "submit and confirm a transaction instead of simulating")
	dryRun := fs.Bool("dry-run", false, "resolve and print the instruction without contacting the network")
	list := fs.Bool("list", false, "list the project's instructions with their arguments and accounts")
	asJSON := fs.Bool("json", false, "print JSON (named instructions; raw -data calls always print JSON)")
	var entries []accountEntry
	var seeds listFlag
	fs.Var(accountFlag{&entries, false}, "account", "named: Name=<address|payer|program>; raw: <address|payer|program>[:w|:s|:ws], in order (repeatable)")
	fs.Var(accountFlag{&entries, true}, "signer", "named: Name=<keypair file>; raw: <keypair file>[:w], a signer account in order (repeatable)")
	fs.Var(&seeds, "seed", "Field=value for a PDA seed the schema cannot supply, e.g. Escrow.Nonce=7 (repeatable)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	rest := fs.Args()
	if *list || (len(rest) == 0 && *data == "") {
		if len(rest) > 0 || *data != "" {
			return fmt.Errorf("-list takes no instruction or -data")
		}
		s, err := loadCallSchema(*dir)
		if err != nil {
			return err
		}
		callplan.List(os.Stdout, s)
		return nil
	}
	var schema *project.ResolvedSchema
	ixArgs := map[string]string{}
	if len(rest) > 0 {
		if *data != "" {
			return fmt.Errorf("give an instruction name or -data, not both")
		}
		var err error
		if schema, err = loadCallSchema(*dir); err != nil {
			return err
		}
		ix, err := callplan.Find(schema, rest[0])
		if err != nil {
			return err
		}
		globals, err := splitCallArgs(fs, schema, ix, rest[1:], ixArgs)
		if err != nil {
			return err
		}
		if err = fs.Parse(globals); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected argument %q after the instruction's flags", fs.Arg(0))
		}
	}
	if *send && *dryRun {
		return fmt.Errorf("choose -send or -dry-run")
	}
	var cluster network.Cluster
	if *name != "" {
		var err error
		if cluster, err = network.Lookup(*name); err != nil {
			return err
		}
		if *url != "" {
			cluster.URL = *url
		}
	} else if !*dryRun || *programID == "" || *payer == "" {
		return fmt.Errorf("pass -network (or, with -dry-run, -program and -keypair)\n%s", callUsage)
	}
	payerSource := "fee payer (-keypair)"
	if *payer == "" {
		root, err := project.Find(*dir)
		if err != nil {
			return fmt.Errorf("pass -keypair, or run inside a project with a deploy payer: %w", err)
		}
		*payer = filepath.Join(root, "deploy", "keys", cluster.Name+"-payer.json")
		payerSource = "fee payer (deploy/keys/" + cluster.Name + "-payer.json)"
	}
	programSource := "-program"
	if *programID == "" {
		root, err := project.Find(*dir)
		if err != nil {
			return err
		}
		record, err := os.ReadFile(filepath.Join(root, "deploy", cluster.Name+".json"))
		if err != nil {
			return fmt.Errorf("no deployment record for %s; deploy first or pass -program: %w", cluster.Name, err)
		}
		var r struct {
			ProgramID string `json:"program_id"`
		}
		if err = json.Unmarshal(record, &r); err != nil {
			return err
		}
		*programID, programSource = r.ProgramID, "deploy/"+cluster.Name+".json"
	}
	program, err := network.DecodeBase58(*programID)
	if err != nil || len(program) != 32 {
		return fmt.Errorf("invalid program ID %q", *programID)
	}
	key, err := network.Keypair(*payer)
	if err != nil {
		return err
	}
	rpc := network.RPC{URL: cluster.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if schema == nil {
		if len(seeds) > 0 {
			return fmt.Errorf("-seed applies to named instructions")
		}
		return rawCall(ctx, rpc, cluster.Name, key, program, *programID, *data, entries, *send, *dryRun)
	}

	in := callplan.Inputs{Program: program, Payer: key.Public().(ed25519.PublicKey), PayerSource: payerSource, Args: ixArgs,
		Accounts: map[string][]byte{}, Signers: map[string]ed25519.PrivateKey{}, Seeds: map[string]string{}, Unsigned: *dryRun}
	for _, e := range entries {
		n, v, ok := strings.Cut(e.value, "=")
		if !ok || n == "" {
			flagName := "account"
			if e.signer {
				flagName = "signer"
			}
			return fmt.Errorf("-%s %q: named instructions take Name=value, e.g. -%s Owner=...", flagName, e.value, flagName)
		}
		if _, dup := in.Accounts[n]; dup || in.Signers[n] != nil {
			return fmt.Errorf("account %s given twice", n)
		}
		if e.signer {
			k, err := network.Keypair(v)
			if err != nil {
				return fmt.Errorf("-signer %s: %w", n, err)
			}
			in.Signers[n] = k
			continue
		}
		k, err := addressValue(v, key, program)
		if err != nil {
			return fmt.Errorf("-account %s: %w", n, err)
		}
		in.Accounts[n] = k
	}
	for _, s := range seeds {
		n, v, ok := strings.Cut(s, "=")
		if !ok {
			return fmt.Errorf("-seed %q: want Field=value, e.g. -seed Escrow.Nonce=7", s)
		}
		in.Seeds[n] = v
	}
	if !*dryRun {
		in.Fetch = func(address []byte) ([]byte, error) {
			_, b, ok, err := rpc.Account(ctx, address)
			if err == nil && !ok {
				err = fmt.Errorf("account does not exist")
			}
			return b, err
		}
	}
	plan, err := callplan.Build(schema, rest[0], in)
	if err != nil {
		return err
	}
	mode := "simulate"
	if *send {
		mode = "send"
	} else if *dryRun {
		mode = "dry run"
	}
	where := cluster.Name
	if where == "" {
		where = "no network"
	}
	if !*asJSON {
		printPlan(os.Stdout, plan, where, mode, *programID, programSource)
	}
	if *dryRun {
		if *asJSON {
			return printJSON(planJSON(plan, cluster.Name, *programID))
		}
		return nil
	}
	out, err := network.InvokeInstructions(ctx, rpc, key, plan.Instructions(), plan.Signers, *send)
	if err != nil {
		return err
	}
	ixIndex := len(plan.Instructions()) - 1
	errName := ""
	if i, code, ok := callplan.CustomError(out.Err); ok && i == ixIndex {
		errName = callplan.ErrorName(schema, code)
	}
	if *asJSON {
		m := planJSON(plan, cluster.Name, *programID)
		for k, v := range outcomeJSON(out) {
			m[k] = v
		}
		m["error_name"] = errName
		return printJSON(m)
	}
	printOutcome(os.Stdout, out, program, ixIndex, errName)
	return nil
}

func loadCallSchema(dir string) (*project.ResolvedSchema, error) {
	root, err := project.Find(dir)
	if err != nil {
		return nil, fmt.Errorf("calling an instruction by name needs a schema-2 project (or use -data <hex>): %w", err)
	}
	c, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	if c.Schema != project.MultiSchema {
		return nil, fmt.Errorf("calling an instruction by name needs a schema-2 project; %s uses schema %d, so pass -data <hex>", root, c.Schema)
	}
	return project.LoadSchema(root)
}

// splitCallArgs separates the instruction's argument flags from call flags
// after the instruction name. An argument named like a call flag (say -data)
// takes precedence there; put call flags before the instruction name to avoid it.
func splitCallArgs(fs *flag.FlagSet, s *project.ResolvedSchema, ix int, tokens []string, ixArgs map[string]string) ([]string, error) {
	var globals []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !strings.HasPrefix(t, "-") || t == "-" || t == "--" {
			return nil, fmt.Errorf("unexpected argument %q; instruction arguments are flags such as -%s", t, firstFlag(s, ix))
		}
		n, v, hasValue := strings.Cut(strings.TrimLeft(t, "-"), "=")
		if f, ok := callplan.ArgField(s, ix, n); ok {
			if !hasValue {
				if i+1 >= len(tokens) {
					return nil, fmt.Errorf("-%s needs a value (%s)", n, f.Type)
				}
				i++
				v = tokens[i]
			}
			if _, dup := ixArgs[n]; dup {
				return nil, fmt.Errorf("-%s given twice", n)
			}
			ixArgs[n] = v
			continue
		}
		g := fs.Lookup(n)
		if g == nil {
			var expected []string
			for _, f := range callplan.Fields(s.Arguments[ix]) {
				expected = append(expected, f.Usage())
			}
			if len(expected) == 0 {
				expected = []string{"no arguments"}
			}
			return nil, fmt.Errorf("%s has no argument -%s; expected: %s", s.Instructions[ix].Name, n, strings.Join(expected, " "))
		}
		globals = append(globals, t)
		if b, ok := g.Value.(interface{ IsBoolFlag() bool }); !hasValue && !(ok && b.IsBoolFlag()) && i+1 < len(tokens) {
			i++
			globals = append(globals, tokens[i])
		}
	}
	return globals, nil
}

func firstFlag(s *project.ResolvedSchema, ix int) string {
	if f := callplan.Fields(s.Arguments[ix]); len(f) > 0 {
		return f[0].Flag
	}
	return "send"
}

func addressValue(v string, payer ed25519.PrivateKey, program []byte) ([]byte, error) {
	switch v {
	case "payer":
		return payer.Public().(ed25519.PublicKey), nil
	case "program":
		return program, nil
	}
	k, err := network.DecodeBase58(v)
	if err != nil || len(k) != 32 {
		return nil, fmt.Errorf("want a base58 address, payer or program, got %q", v)
	}
	return k, nil
}

// rawCall keeps the original -data behaviour: without -account/-signer the
// payer is the only account; with them, the accounts are exactly those given.
func rawCall(ctx context.Context, rpc network.RPC, networkName string, key ed25519.PrivateKey, program []byte, programID, data string, entries []accountEntry, send, dryRun bool) error {
	input, err := hex.DecodeString(data)
	if err != nil {
		return fmt.Errorf("-data must be hex: %w", err)
	}
	var metas []network.Meta
	var signers []ed25519.PrivateKey
	for _, e := range entries {
		if strings.Contains(e.value, "=") {
			return fmt.Errorf("Name=value accounts need an instruction name; with -data use -account <address>[:w|:s|:ws] or -signer <keypair>[:w]")
		}
		v, flags, _ := strings.Cut(e.value, ":")
		if e.signer {
			if flags != "" && flags != "w" {
				return fmt.Errorf("-signer %q: the only flag is :w (writable)", e.value)
			}
			k, err := network.Keypair(v)
			if err != nil {
				return err
			}
			signers = append(signers, k)
			metas = append(metas, network.Meta{Key: k.Public().(ed25519.PublicKey), Signer: true, Writable: flags == "w"})
			continue
		}
		if strings.Trim(flags, "ws") != "" || len(flags) > 2 {
			return fmt.Errorf("-account %q: flags are :w (writable), :s (signer) or :ws", e.value)
		}
		k, err := addressValue(v, key, program)
		if err != nil {
			return fmt.Errorf("-account %q: %w", e.value, err)
		}
		if strings.Contains(flags, "s") && !bytes.Equal(k, key.Public().(ed25519.PublicKey)) && !dryRun {
			return fmt.Errorf("-account %q signs, so pass its keypair with -signer instead", e.value)
		}
		metas = append(metas, network.Meta{Key: k, Signer: strings.Contains(flags, "s"), Writable: strings.Contains(flags, "w")})
	}
	accounts := func() []map[string]any {
		var a []map[string]any
		for _, m := range metas {
			a = append(a, map[string]any{"address": network.Base58(m.Key), "signer": m.Signer, "writable": m.Writable})
		}
		return a
	}
	if dryRun {
		if len(metas) == 0 {
			metas = []network.Meta{{Key: key.Public().(ed25519.PublicKey), Signer: true, Writable: true}}
		}
		return printJSON(map[string]any{"network": networkName, "program_id": programID, "input_hex": data, "accounts": accounts()})
	}
	var out network.Outcome
	if len(metas) == 0 {
		out, err = network.Invoke(ctx, rpc, key, programID, input, send)
	} else {
		out, err = network.InvokeInstructions(ctx, rpc, key, []network.Instruction{{Program: program, Accounts: metas, Data: input}}, signers, send)
	}
	if err != nil {
		return err
	}
	m := outcomeJSON(out)
	m["network"], m["program_id"], m["input_hex"] = networkName, programID, data
	if len(metas) > 0 {
		m["accounts"] = accounts()
	}
	return printJSON(m)
}

func outcomeJSON(out network.Outcome) map[string]any {
	return map[string]any{"signature": out.Signature, "committed": out.Committed, "error": out.Err, "return_data_hex": hex.EncodeToString(out.ReturnData),
		"return_program": out.ReturnSource, "compute_units": out.ComputeUnits, "logs": out.Logs}
}

func planJSON(p *callplan.Plan, networkName, programID string) map[string]any {
	args := map[string]string{}
	for _, a := range p.Args {
		args[a.Flag] = a.Format(a.Value)
	}
	var accounts []map[string]any
	for _, a := range p.Accounts {
		accounts = append(accounts, map[string]any{"name": a.Name, "address": network.Base58(a.Key), "signer": a.Signer, "writable": a.Writable, "source": a.Source})
	}
	return map[string]any{"network": networkName, "program_id": programID, "instruction": p.Instruction, "args": args, "accounts": accounts, "input_hex": hex.EncodeToString(p.Data)}
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func printPlan(w io.Writer, p *callplan.Plan, where, mode, programID, programSource string) {
	fmt.Fprintf(w, "%s on %s (%s)\n", p.Instruction, where, mode)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  program\t%s\t%s\n", programID, programSource)
	fmt.Fprintf(tw, "  data\t%s\n", hex.EncodeToString(p.Data))
	if p.HeapBytes > 32*1024 {
		fmt.Fprintf(tw, "  heap\tRequestHeapFrame %d bytes first\t(gosvm.json heap_bytes)\n", p.HeapBytes)
	}
	tw.Flush()
	if len(p.Args) > 0 {
		fmt.Fprintln(w, "  args")
		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, a := range p.Args {
			if a.Source == "" {
				fmt.Fprintf(tw, "    -%s\t%s\n", a.Flag, a.Format(a.Value))
			} else {
				fmt.Fprintf(tw, "    -%s\t%s\t%s\n", a.Flag, a.Format(a.Value), a.Source)
			}
		}
		tw.Flush()
	}
	fmt.Fprintln(w, "  accounts")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, a := range p.Accounts {
		fmt.Fprintf(tw, "    %s\t%s\t%s\t%s\n", a.Name, callplan.Privileges(a.Writable, a.Signer), network.Base58(a.Key), a.Source)
	}
	tw.Flush()
	for _, warning := range p.Warnings {
		fmt.Fprintf(w, "  warning: %s\n", warning)
	}
}

func printOutcome(w io.Writer, out network.Outcome, program []byte, ixIndex int, errName string) {
	verb := "simulated"
	if out.Committed {
		verb = "committed"
	}
	failed := len(out.Err) > 0 && string(out.Err) != "null"
	status := "ok"
	if failed {
		status = "failed"
	}
	fmt.Fprintf(w, "%s: %s, %d compute units\n", verb, status, out.ComputeUnits)
	if out.Signature != "" {
		fmt.Fprintf(w, "  signature    %s\n", out.Signature)
	}
	if failed {
		if i, code, ok := callplan.CustomError(out.Err); ok && i == ixIndex {
			name := ""
			if errName != "" {
				name = " " + errName
			}
			fmt.Fprintf(w, "  error        custom %d%s  %s\n", code, name, out.Err)
		} else {
			fmt.Fprintf(w, "  error        %s\n", out.Err)
		}
	}
	if len(out.ReturnData) > 0 {
		decoded := ""
		if len(out.ReturnData) == 8 {
			decoded = fmt.Sprintf(" (as little-endian uint64: %d)", binary.LittleEndian.Uint64(out.ReturnData))
		}
		from := ""
		if source, _ := network.DecodeBase58(out.ReturnSource); !bytes.Equal(source, program) {
			from = " from " + out.ReturnSource
		}
		fmt.Fprintf(w, "  return data  %s%s%s\n", hex.EncodeToString(out.ReturnData), decoded, from)
	}
	if len(out.Logs) > 0 {
		fmt.Fprintln(w, "  logs")
		for _, l := range out.Logs {
			fmt.Fprintf(w, "    %s\n", l)
		}
	}
}
