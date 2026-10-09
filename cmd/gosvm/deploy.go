package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gosvm/internal/network"
	"gosvm/internal/project"
	"gosvm/internal/targets"
)

// deployCommand builds the network's target, confirms from live feature
// accounts that the cluster accepts that program format, then deploys it
// natively through the upgradeable loader. Mainnets require -mainnet.
func deployCommand(args []string) error {
	fs := flag.NewFlagSet("gosvm deploy", flag.ContinueOnError)
	name := fs.String("network", "", "fogo-testnet, fogo-mainnet, solana-devnet, solana-testnet or solana-mainnet")
	dir := fs.String("dir", ".", "project directory")
	payer := fs.String("keypair", "", "fee payer keypair (default: deploy/keys/<network>-payer.json, created if missing)")
	authorityPath := fs.String("authority", "", "upgrade authority keypair (default: the payer)")
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
	// Never fall back to the Solana CLI's configured keypair: it may be a
	// production authority for an unrelated program.
	if *payer == "" {
		*payer = filepath.Join(root, "deploy", "keys", cluster.Name+"-payer.json")
		k, created, err := network.EnsureKeypair(*payer)
		if err != nil {
			return err
		}
		if created {
			fmt.Printf("Created deployment payer %s (address %s)\n", *payer, network.Address(k))
		}
	}
	payerKey, err := network.Keypair(*payer)
	if err != nil {
		return err
	}
	authorityKey := payerKey
	if *authorityPath != "" {
		if authorityKey, err = network.Keypair(*authorityPath); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(program)
	if err != nil {
		return err
	}
	var balance struct {
		Value uint64 `json:"value"`
	}
	var rent uint64
	if err = rpc.Call(ctx, "getBalance", []any{network.Address(payerKey)}, &balance); err != nil {
		return err
	}
	// Program data account (ELF + 45-byte header), a same-size temporary buffer
	// (refunded) and the 36-byte program account, plus a fee margin.
	if err = rpc.Call(ctx, "getMinimumBalanceForRentExemption", []any{len(data) + 45}, &rent); err != nil {
		return err
	}
	need := 2*rent + 10_000_000
	if balance.Value < need {
		return fmt.Errorf("payer %s has %.6f on %s; deployment needs about %.6f. Fund it (%s), then rerun", network.Address(payerKey), float64(balance.Value)/1e9, cluster.Name, float64(need)/1e9, cluster.Faucet)
	}
	keyPath := filepath.Join(root, "deploy", "keys", cluster.Name+"-program.json")
	key, created, err := network.EnsureKeypair(keyPath)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("Created program keypair %s (address %s); keep it to redeploy at the same address\n", keyPath, network.Address(key))
	}
	deployCtx, deployCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer deployCancel()
	bufferPath := filepath.Join(root, "deploy", "keys", cluster.Name+"-buffer.json")
	if err = rpc.ReclaimBuffers(deployCtx, payerKey, authorityKey, os.Stdout); err != nil {
		return err
	}
	exists, _, _, err := rpc.Deployed(deployCtx, []byte(key.Public().(ed25519.PublicKey)))
	if err != nil {
		return err
	}
	action := "Deployed"
	var signature string
	if exists {
		action = "Upgraded"
		signature, err = network.UpgradeV3(deployCtx, rpc, payerKey, authorityKey, []byte(key.Public().(ed25519.PublicKey)), data, bufferPath, os.Stdout)
		if err == nil && signature == "" {
			return nil
		}
	} else {
		signature, err = network.DeployV3(deployCtx, rpc, payerKey, authorityKey, key, data, bufferPath, os.Stdout)
	}
	if err != nil {
		return fmt.Errorf("deploy to %s: %w", cluster.Name, err)
	}
	record := map[string]any{"network": cluster.Name, "url": cluster.URL, "genesis_hash": genesis, "target_profile": p.Name, "platform_tools": p.Tools,
		"program_id": network.Address(key), "payer": network.Address(payerKey), "upgrade_authority": network.Address(authorityKey), "signature": signature, "elf_sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "elf_bytes": len(data), "deployed_at": time.Now().UTC().Format(time.RFC3339)}
	recordPath := filepath.Join(root, "deploy", cluster.Name+".json")
	// Keep earlier deployments of this program as history.
	if old, err := os.ReadFile(recordPath); err == nil && exists {
		var previous map[string]any
		if json.Unmarshal(old, &previous) == nil && previous["program_id"] == record["program_id"] {
			history, _ := previous["history"].([]any)
			delete(previous, "history")
			record["history"] = append(history, previous)
		}
	}
	recordData, _ := json.MarshalIndent(record, "", "  ")
	if err = os.WriteFile(recordPath, append(recordData, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("%s %s on %s at %s\nRecord: %s\n", action, p.Network(), cluster.Name, network.Address(key), recordPath)
	return nil
}
