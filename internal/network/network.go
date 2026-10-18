// Package network names public SVM clusters and checks, from their live
// feature accounts, that a target profile can be deployed there.
package network

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gosvm/internal/targets"
)

type Cluster struct {
	Name    string
	URL     string
	Profile string
	Mainnet bool
	// Token is the native token that pays rent and fees (SOL or FOGO).
	Token string
	// Faucet is the public faucet for a test network; empty on mainnets.
	Faucet string
}

var clusters = map[string]Cluster{
	"fogo-testnet":   {"fogo-testnet", "https://testnet.fogo.io", target.Fogo, false, "FOGO", "https://faucet.fogo.io"},
	"fogo-mainnet":   {"fogo-mainnet", "https://mainnet.fogo.io", target.Fogo, true, "FOGO", ""},
	"solana-devnet":  {"solana-devnet", "https://api.devnet.solana.com", target.Solana, false, "SOL", "https://faucet.solana.com"},
	"solana-testnet": {"solana-testnet", "https://api.testnet.solana.com", target.Solana, false, "SOL", "https://faucet.solana.com"},
	"solana-mainnet": {"solana-mainnet", "https://api.mainnet-beta.solana.com", target.Solana, true, "SOL", ""},
}

// FundingMessage explains that payer holds balance lamports but a deployment
// needs about need, in the cluster's native token, and how to fund it. Faucet
// hints appear only on test networks.
func (c Cluster) FundingMessage(payer string, balance, need uint64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "payer %s has %.6f %s on %s; deployment needs about %.6f %s.\n", payer, float64(balance)/1e9, c.Token, c.Name, float64(need)/1e9, c.Token)
	switch {
	case c.Mainnet || c.Faucet == "":
		fmt.Fprintf(&b, "Transfer at least %.6f %s to %s, then rerun.", float64(need-balance)/1e9, c.Token, payer)
	case c.Profile == target.Solana:
		network := strings.TrimPrefix(c.Name, "solana-")
		// A whole-token airdrop covering the shortfall.
		amount := (need - balance + 999_999_999) / 1_000_000_000
		fmt.Fprintf(&b, "Fund it, then rerun:\n")
		fmt.Fprintf(&b, "  %s (choose %s; sign in with GitHub for a larger allowance;\n", c.Faucet, network)
		fmt.Fprintf(&b, "    public airdrops are rate-limited per IP address per day)\n")
		fmt.Fprintf(&b, "  or: solana airdrop %d %s --url %s", amount, payer, c.URL)
	default:
		fmt.Fprintf(&b, "Fund it from %s, then rerun.", c.Faucet)
	}
	return b.String()
}

func Names() []string {
	var names []string
	for n := range clusters {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func Lookup(name string) (Cluster, error) {
	c, ok := clusters[name]
	if !ok {
		return c, fmt.Errorf("unknown network %q; choose one of %s", name, strings.Join(Names(), ", "))
	}
	return c, nil
}

// Feature gates governing program deployment (agave feature IDs).
const (
	// SIMD-0178/0179/0189: deployment and execution of SBPFv3 programs.
	FeatureSBPFv3 = "5cC3foj77CWun58pC51ebHFUWavHWKarWyR5UUik7dnC"
	// SIMD-0500: disables deployment of SBPFv0/v1/v2 programs.
	FeatureDisableV0 = "B8JJXCy5amZyWG9r7EnUYLwzXSXTxG7GZ1qZ1qggo83g"
)

// RPC is a minimal JSON-RPC client.
type RPC struct {
	URL    string
	Client *http.Client
}

// Call performs a JSON-RPC request, retrying transport failures and HTTP 429/5xx
// (public endpoints reset connections and rate-limit). Every method GoSVM uses
// is safe to repeat: reads are idempotent and a resent signed transaction is
// deduplicated by its signature.
func (r RPC) Call(ctx context.Context, method string, params []any, result any) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		var retry bool
		if retry, err = r.call(ctx, method, params, result); err == nil || !retry {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return err
}

func (r RPC) call(ctx context.Context, method string, params []any, result any) (bool, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, "POST", r.URL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return true, fmt.Errorf("%s %s: HTTP %s", r.URL, method, resp.Status)
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return true, fmt.Errorf("%s %s: %w", r.URL, method, err)
	}
	if out.Error != nil {
		return false, fmt.Errorf("%s %s: %s", r.URL, method, out.Error.Message)
	}
	return false, json.Unmarshal(out.Result, result)
}

// FeatureActive reports whether a feature account exists with an activation slot.
func (r RPC) FeatureActive(ctx context.Context, id string) (bool, error) {
	var res struct {
		Value *struct {
			Data []string `json:"data"`
		} `json:"value"`
	}
	if err := r.Call(ctx, "getAccountInfo", []any{id, map[string]any{"encoding": "base64"}}, &res); err != nil {
		return false, err
	}
	if res.Value == nil || len(res.Value.Data) == 0 {
		return false, nil
	}
	data, err := base64.StdEncoding.DecodeString(res.Value.Data[0])
	if err != nil {
		return false, err
	}
	return len(data) >= 9 && data[0] == 1, nil
}

// CheckDeployable verifies from live feature accounts that the cluster accepts
// programs of this profile, so users never need to know the ISA rules.
func CheckDeployable(ctx context.Context, r RPC, profile target.Profile) error {
	v3, err := r.FeatureActive(ctx, FeatureSBPFv3)
	if err != nil {
		return err
	}
	noV0, err := r.FeatureActive(ctx, FeatureDisableV0)
	if err != nil {
		return err
	}
	switch {
	case profile.Arch == "v3" && profile.Name == target.Solana && !v3:
		return fmt.Errorf("%s does not accept SBPFv3 programs (feature %s inactive); deploy the fogo target instead", r.URL, FeatureSBPFv3)
	case profile.Arch == "v0" && noV0:
		return fmt.Errorf("%s no longer accepts SBPFv0 deployments (feature %s active); deploy the solana target instead", r.URL, FeatureDisableV0)
	case profile.Name == target.Legacy:
		return fmt.Errorf("the historical %s profile is not deployable; migrate the project to fogo/solana", target.Legacy)
	}
	return nil
}

// Keypair loads a Solana CLI keypair file (JSON array of 64 bytes).
func Keypair(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var ints []int
	if err = json.Unmarshal(data, &ints); err != nil {
		return nil, fmt.Errorf("%s: not a Solana keypair file", path)
	}
	for _, v := range ints {
		raw = append(raw, byte(v))
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s: not a Solana keypair file", path)
	}
	key := ed25519.PrivateKey(raw)
	if !bytes.Equal(ed25519.NewKeyFromSeed(key.Seed()).Public().(ed25519.PublicKey), key.Public().(ed25519.PublicKey)) {
		return nil, fmt.Errorf("%s: inconsistent keypair", path)
	}
	return key, nil
}

// EnsureKeypair returns the keypair at path, creating a new one (0600, in a
// directory ignored by Git) if it does not exist.
func EnsureKeypair(path string) (ed25519.PrivateKey, bool, error) {
	if _, err := os.Stat(path); err == nil {
		k, err := Keypair(path)
		return k, false, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, false, err
	}
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignore); os.IsNotExist(err) {
		if err = os.WriteFile(ignore, []byte("*\n"), 0644); err != nil {
			return nil, false, err
		}
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, false, err
	}
	ints := make([]int, len(key))
	for i, b := range key {
		ints[i] = int(b)
	}
	data, _ := json.Marshal(ints)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, false, err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return nil, false, err
	}
	return key, true, f.Close()
}

func Address(key ed25519.PrivateKey) string { return Base58(key.Public().(ed25519.PublicKey)) }

func Base58(b []byte) string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int).SetBytes(b)
	base, rem := big.NewInt(58), new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, rem)
		out = append(out, alphabet[rem.Int64()])
	}
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}
