package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gosvm/internal/network"
	"gosvm/internal/project"
	"gosvm/internal/targets"
)

// deployCommand builds the network's target, confirms from live feature
// accounts that the cluster accepts that program format, then deploys it with
// the Solana CLI. Mainnet deployment requires an explicit -mainnet flag.
func deployCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm deploy", flag.ContinueOnError)
	name := fs.String("network", "", "fogo-testnet, fogo-mainnet, solana-devnet, solana-testnet or solana-mainnet")
	dir := fs.String("dir", ".", "project directory")
	payer := fs.String("keypair", "", "fee payer / upgrade authority keypair (default: Solana CLI configured keypair)")
	url := fs.String("url", "", "override the network's RPC URL")
	mainnet := fs.Bool("mainnet", false, "confirm a mainnet deployment")
	check := fs.Bool("check", false, "only build and check the cluster accepts the program; do not deploy")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || *name == "" {
		return fmt.Errorf("usage: gosvm deploy -network <%s> [-keypair payer.json] [-check]", "fogo-testnet|solana-devnet|...")
	}
	cluster, err := network.Lookup(*name)
	if err != nil {
		return err
	}
	if *url != "" {
		cluster.URL = *url
	}
	if cluster.Mainnet && !*mainnet && !*check {
		return fmt.Errorf("%s is a mainnet: add -mainnet to confirm a real deployment", cluster.Name)
	}
	root, err := project.Find(*dir)
	if err != nil {
		return err
	}
	c, err := project.Load(root)
	if err != nil {
		return err
	}
	profiles, err := c.TargetProfiles()
	if err != nil {
		return err
	}
	p, _ := target.Resolve(cluster.Profile)
	configured := false
	for _, q := range profiles {
		configured = configured || q.Name == p.Name
	}
	if !configured {
		return fmt.Errorf("project does not target %s; add it with gosvm migrate-target -target fogo,solana", p.Network())
	}
	if err = projectCommand([]string{"build", "-target", p.Network(), "-dir", root}); err != nil {
		return err
	}
	program := filepath.Join(root, "build", p.Network(), "program.so")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rpc := network.RPC{URL: cluster.URL}
	if err = network.CheckDeployable(ctx, rpc, p); err != nil {
		return err
	}
	var genesis string
	if err = rpc.Call(ctx, "getGenesisHash", nil, &genesis); err != nil {
		return err
	}
	fmt.Printf("%s (%s) accepts %s programs\n", cluster.Name, cluster.URL, p.Name)
	if *check {
		return nil
	}
	if _, err = exec.LookPath("solana"); err != nil {
		return fmt.Errorf("deployment uses the Solana CLI; install it from https://solana.com/docs/intro/installation")
	}
	keyPath := filepath.Join(root, "deploy", "keys", cluster.Name+"-program.json")
	key, created, err := network.EnsureKeypair(keyPath)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("Created program keypair %s (address %s); keep it to redeploy at the same address\n", keyPath, network.Address(key))
	}
	// GoSVM checked the profile against live feature accounts above; older
	// Solana CLIs do not know newer gates, so their local verification is skipped.
	cmdArgs := []string{"program", "deploy", program, "--url", cluster.URL, "--program-id", keyPath, "--skip-feature-verify", "--output", "json"}
	if *payer != "" {
		cmdArgs = append(cmdArgs, "--keypair", *payer)
	}
	cmd := exec.Command("solana", cmdArgs...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("solana program deploy failed: %w\n%s", err, out)
	}
	var result struct {
		ProgramID string `json:"programId"`
		Signature string `json:"signature"`
	}
	json.Unmarshal(out, &result)
	data, err := os.ReadFile(program)
	if err != nil {
		return err
	}
	record := map[string]any{"network": cluster.Name, "url": cluster.URL, "genesis_hash": genesis, "target_profile": p.Name, "platform_tools": p.Tools,
		"program_id": network.Address(key), "signature": result.Signature, "elf_sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "elf_bytes": len(data), "deployed_at": time.Now().UTC().Format(time.RFC3339)}
	recordData, _ := json.MarshalIndent(record, "", "  ")
	recordPath := filepath.Join(root, "deploy", cluster.Name+".json")
	if err = os.WriteFile(recordPath, append(recordData, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("Deployed %s to %s at %s\nRecord: %s\n", p.Network(), cluster.Name, network.Address(key), recordPath)
	return nil
}
