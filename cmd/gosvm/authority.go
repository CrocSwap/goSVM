package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gosvm/internal/network"
	"gosvm/internal/project"
)

// authorityCommand shows or changes a deployed program's upgrade authority.
func authorityCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm authority", flag.ContinueOnError)
	name := fs.String("network", "", "network the program was deployed to")
	dir := fs.String("dir", ".", "project directory")
	url := fs.String("url", "", "override the network's RPC URL")
	payerPath := fs.String("keypair", "", "fee payer keypair (default: deploy/keys/<network>-payer.json)")
	currentPath := fs.String("authority", "", "current upgrade authority keypair (default: the payer)")
	set := fs.String("set", "", "transfer to this keypair file; the new authority co-signs (SetAuthorityChecked)")
	setAddress := fs.String("set-address", "", "transfer to an address that cannot sign here (e.g. a multisig); requires -unchecked")
	unchecked := fs.Bool("unchecked", false, "confirm an address-only transfer that the new authority does not co-sign")
	immutable := fs.Bool("immutable", false, "remove the upgrade authority: the program can never be upgraded again")
	confirm := fs.String("confirm", "", "for -immutable: repeat the program ID to confirm")
	mainnet := fs.Bool("mainnet", false, "confirm a change on a mainnet")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || *name == "" {
		return fmt.Errorf("usage: gosvm authority -network N [-set new.json | -set-address ADDR -unchecked | -immutable -confirm PROGRAM_ID] [-authority current.json]")
	}
	cluster, err := network.Lookup(*name)
	if err != nil {
		return err
	}
	if *url != "" {
		cluster.URL = *url
	}
	root, err := project.Find(*dir)
	if err != nil {
		return err
	}
	recordPath := filepath.Join(root, "deploy", cluster.Name+".json")
	recordData, err := os.ReadFile(recordPath)
	if err != nil {
		return fmt.Errorf("no deployment record for %s: %w", cluster.Name, err)
	}
	var record map[string]any
	if err = json.Unmarshal(recordData, &record); err != nil {
		return err
	}
	programID, _ := record["program_id"].(string)
	programKey, err := network.DecodeBase58(programID)
	if err != nil || len(programKey) != 32 {
		return fmt.Errorf("invalid program_id in %s", recordPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rpc := network.RPC{URL: cluster.URL}
	exists, current, _, err := rpc.Deployed(ctx, programKey)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("program %s does not exist on %s", programID, cluster.Name)
	}
	describe := func(a []byte) string {
		if a == nil {
			return "none (immutable)"
		}
		return network.Base58(a)
	}
	changes := 0
	for _, on := range []bool{*set != "", *setAddress != "", *immutable} {
		if on {
			changes++
		}
	}
	if changes == 0 {
		fmt.Printf("Program %s on %s: upgrade authority %s\n", programID, cluster.Name, describe(current))
		// Resynchronize the record with the chain (e.g. after an interrupted change).
		var onChain any
		if current != nil {
			onChain = network.Base58(current)
		}
		if record["upgrade_authority"] != onChain {
			record["upgrade_authority"] = onChain
			out, _ := json.MarshalIndent(record, "", "  ")
			return os.WriteFile(recordPath, append(out, '\n'), 0644)
		}
		return nil
	}
	if changes > 1 {
		return fmt.Errorf("choose one of -set, -set-address or -immutable")
	}
	if cluster.Mainnet && !*mainnet {
		return fmt.Errorf("%s is a mainnet: add -mainnet to confirm", cluster.Name)
	}
	if *payerPath == "" {
		*payerPath = filepath.Join(root, "deploy", "keys", cluster.Name+"-payer.json")
	}
	payer, err := network.Keypair(*payerPath)
	if err != nil {
		return err
	}
	authority := payer
	if *currentPath != "" {
		if authority, err = network.Keypair(*currentPath); err != nil {
			return err
		}
	}
	var change network.AuthorityChange
	switch {
	case *set != "":
		if change.NewKey, err = network.Keypair(*set); err != nil {
			return err
		}
	case *setAddress != "":
		if !*unchecked {
			return fmt.Errorf("-set-address transfers to a key that does not co-sign; a wrong address locks the program forever. Add -unchecked to confirm, or use -set with a keypair")
		}
		if change.NewAddress, err = network.DecodeBase58(*setAddress); err != nil || len(change.NewAddress) != 32 {
			return fmt.Errorf("invalid -set-address %q", *setAddress)
		}
	case *immutable:
		if *confirm != programID {
			return fmt.Errorf("-immutable is permanent; repeat the program ID with -confirm %s", programID)
		}
		change.Immutable = true
	}
	signature, err := network.SetAuthority(ctx, rpc, payer, authority, programKey, change)
	if err != nil {
		return err
	}
	_, now, _, err := rpc.Deployed(ctx, programKey)
	if err != nil {
		return err
	}
	record["upgrade_authority"] = nil
	if now != nil {
		record["upgrade_authority"] = network.Base58(now)
	}
	changesLog, _ := record["authority_changes"].([]any)
	record["authority_changes"] = append(changesLog, map[string]any{"from": network.Base58(current), "to": record["upgrade_authority"], "signature": signature, "at": time.Now().UTC().Format(time.RFC3339)})
	out, _ := json.MarshalIndent(record, "", "  ")
	if err = os.WriteFile(recordPath, append(out, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("Program %s on %s: upgrade authority %s -> %s\n", programID, cluster.Name, network.Base58(current), describe(now))
	return nil
}
