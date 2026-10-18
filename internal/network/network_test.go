package network

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gosvm/internal/targets"
)

func fakeCluster(t *testing.T, active map[string]bool) RPC {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params []any `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var value any
		if active[req.Params[0].(string)] {
			data := make([]byte, 9)
			data[0], data[1] = 1, 42
			value = map[string]any{"data": []string{base64.StdEncoding.EncodeToString(data), "base64"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"value": value}})
	}))
	t.Cleanup(server.Close)
	return RPC{URL: server.URL}
}

func TestCheckDeployableFollowsLiveFeatures(t *testing.T) {
	fogo, _ := target.Resolve("fogo")
	solana, _ := target.Resolve("solana")
	legacy, _ := target.Resolve(target.Legacy)
	ctx := context.Background()
	for _, c := range []struct {
		active  map[string]bool
		profile target.Profile
		ok      bool
	}{
		{map[string]bool{}, fogo, true},
		{map[string]bool{}, solana, false},
		{map[string]bool{FeatureSBPFv3: true}, solana, true},
		{map[string]bool{FeatureSBPFv3: true}, fogo, true},
		{map[string]bool{FeatureSBPFv3: true, FeatureDisableV0: true}, fogo, false},
		{map[string]bool{FeatureSBPFv3: true}, legacy, false},
	} {
		err := CheckDeployable(ctx, fakeCluster(t, c.active), c.profile)
		if (err == nil) != c.ok {
			t.Fatalf("%v %s: %v", c.active, c.profile.Name, err)
		}
	}
}

func TestEnsureKeypairRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "program.json")
	k, created, err := EnsureKeypair(path)
	if err != nil || !created {
		t.Fatal(err)
	}
	again, created, err := EnsureKeypair(path)
	if err != nil || created || Address(again) != Address(k) {
		t.Fatal(err, created)
	}
	if Base58([]byte{0, 0, 1}) != "112" {
		t.Fatal(Base58([]byte{0, 0, 1}))
	}
}

func TestFundingMessage(t *testing.T) {
	const payer = "CmUSAqkhVTMR63AHPYNJUHAJVMSCSybJyt1i9ucaK3hT"
	devnet, _ := Lookup("solana-devnet")
	got := devnet.FundingMessage(payer, 0, 162_370_000)
	want := "payer " + payer + " has 0.000000 SOL on solana-devnet; deployment needs about 0.162370 SOL.\n" +
		"Fund it, then rerun:\n" +
		"  https://faucet.solana.com (choose devnet; sign in with GitHub for a larger allowance;\n" +
		"    public airdrops are rate-limited per IP address per day)\n" +
		"  or: solana airdrop 1 " + payer + " --url https://api.devnet.solana.com"
	if got != want {
		t.Fatalf("devnet message:\n%s\nwant:\n%s", got, want)
	}
	testnet, _ := Lookup("solana-testnet")
	if got := testnet.FundingMessage(payer, 500_000_000, 2_800_000_000); !strings.Contains(got, "choose testnet") || !strings.Contains(got, "solana airdrop 3 "+payer+" --url https://api.testnet.solana.com") {
		t.Fatalf("testnet message: %s", got)
	}
	fogo, _ := Lookup("fogo-testnet")
	if got, want := fogo.FundingMessage(payer, 0, 162_370_000), "payer "+payer+" has 0.000000 FOGO on fogo-testnet; deployment needs about 0.162370 FOGO.\nFund it from https://faucet.fogo.io, then rerun."; got != want {
		t.Fatalf("fogo-testnet message:\n%s\nwant:\n%s", got, want)
	}
	for _, name := range []string{"solana-mainnet", "fogo-mainnet"} {
		c, _ := Lookup(name)
		got := c.FundingMessage(payer, 0, 162_370_000)
		if strings.Contains(got, "faucet") || strings.Contains(got, "airdrop") || !strings.Contains(got, "Transfer at least 0.162370 "+c.Token+" to "+payer) {
			t.Fatalf("%s message: %s", name, got)
		}
	}
}
