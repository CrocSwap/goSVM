package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gosvm/internal/network"
	"gosvm/internal/project"
)

// callCommand invokes a deployed program with instruction data, passing the
// payer as the only (signer) account. It simulates by default; -send commits a transaction.
func callCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm call", flag.ContinueOnError)
	name := fs.String("network", "", "network the program was deployed to")
	dir := fs.String("dir", ".", "project directory (for the deploy record)")
	programID := fs.String("program", "", "program ID (default: from deploy/<network>.json)")
	data := fs.String("data", "", "instruction data, hex")
	payer := fs.String("keypair", "", "fee payer keypair (default: the project's deploy/keys/<network>-payer.json)")
	url := fs.String("url", "", "override the network's RPC URL")
	send := fs.Bool("send", false, "submit and confirm a transaction instead of simulating")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || *name == "" {
		return fmt.Errorf("usage: gosvm call -network <network> -data <hex> [-program ID] [-send]")
	}
	cluster, err := network.Lookup(*name)
	if err != nil {
		return err
	}
	if *url != "" {
		cluster.URL = *url
	}
	if *payer == "" {
		root, err := project.Find(*dir)
		if err != nil {
			return fmt.Errorf("pass -keypair, or run inside a project with a deploy payer: %w", err)
		}
		*payer = filepath.Join(root, "deploy", "keys", cluster.Name+"-payer.json")
	}
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
		*programID = r.ProgramID
	}
	input, err := hex.DecodeString(*data)
	if err != nil {
		return fmt.Errorf("-data must be hex: %w", err)
	}
	key, err := network.Keypair(*payer)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := network.Invoke(ctx, network.RPC{URL: cluster.URL}, key, *programID, input, *send)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(map[string]any{"network": cluster.Name, "program_id": *programID, "input_hex": *data, "signature": out.Signature, "committed": out.Committed,
		"error": out.Err, "return_data_hex": hex.EncodeToString(out.ReturnData), "return_program": out.ReturnSource, "compute_units": out.ComputeUnits, "logs": out.Logs}, "", "  ")
	fmt.Println(string(b))
	return nil
}
