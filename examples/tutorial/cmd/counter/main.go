// Command counter creates, increments and reads a deployed counter from
// ordinary Go, using the program's own model and generated client packages.
//
//	go run ./cmd/counter create 10
//	go run ./cmd/counter increment 3
//	go run ./cmd/counter show
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"example.com/counter/client"
	"example.com/counter/model"
	"example.com/counter/solrpc"
)

func main() {
	url := flag.String("url", "https://api.devnet.solana.com", "RPC URL")
	keypair := flag.String("keypair", "deploy/keys/solana-devnet-payer.json", "counter owner and fee payer")
	record := flag.String("record", "deploy/solana-devnet.json", "the deploy record naming the program")
	flag.Parse()
	log.SetFlags(0)

	owner, err := solrpc.LoadKeypair(*keypair)
	if err != nil {
		log.Fatal(err)
	}
	programID, err := deployedProgram(*record)
	if err != nil {
		log.Fatal(err)
	}
	ownerKey := solrpc.KeyOf(owner)
	counterKey, bump, err := solrpc.FindProgramAddress(programID, []byte("counter"), ownerKey[:])
	if err != nil {
		log.Fatal(err)
	}
	rpc := solrpc.Client{URL: *url}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch flag.Arg(0) {
	case "create":
		args := model.CreateArgs{Limit: number(flag.Arg(1)), Bump: bump}
		send(ctx, rpc, owner, solrpc.Instruction{
			Program: programID,
			Accounts: []solrpc.AccountMeta{
				{Key: counterKey, Writable: true},
				{Key: ownerKey, Signer: true, Writable: true},
				{Key: solrpc.SystemProgram},
			},
			Data: client.EncodeInstructionCreate(args),
		})
	case "increment":
		amount := number(flag.Arg(1))
		counter := read(ctx, rpc, counterKey)
		// The same rule the program enforces, checked before paying a fee.
		next, code := model.Next(counter.Count, counter.Limit, amount)
		if code != 0 {
			log.Fatalf("the program would reject this increment with error %d", code)
		}
		fmt.Printf("expect count %d\n", next)
		send(ctx, rpc, owner, solrpc.Instruction{
			Program: programID,
			Accounts: []solrpc.AccountMeta{
				{Key: counterKey, Writable: true},
				{Key: ownerKey, Signer: true},
			},
			Data: client.EncodeInstructionIncrement(model.IncrementArgs{Amount: amount}),
		})
	case "show":
	default:
		log.Fatal("usage: counter [flags] create <limit> | increment <amount> | show")
	}
	counter := read(ctx, rpc, counterKey)
	fmt.Printf("counter %s: count %d of %d\n", counterKey, counter.Count, counter.Limit)
}

func send(ctx context.Context, rpc solrpc.Client, owner ed25519.PrivateKey, ix solrpc.Instruction) {
	result, err := rpc.Send(ctx, ix, owner)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("transaction %s\n  https://explorer.solana.com/tx/%s?cluster=devnet\n  %d compute units, error %s\n",
		result.Signature, result.Signature, result.ComputeUnits, result.Err)
}

func read(ctx context.Context, rpc solrpc.Client, key solrpc.PublicKey) model.Counter {
	data, err := rpc.AccountData(ctx, key)
	if err != nil {
		log.Fatal(err)
	}
	if data == nil {
		log.Fatalf("no counter at %s yet; run create first", key)
	}
	counter, err := client.DecodeAccountCounter(data)
	if err != nil {
		log.Fatal(err)
	}
	return counter
}

func deployedProgram(path string) (solrpc.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return solrpc.PublicKey{}, fmt.Errorf("%w (deploy first, or pass -record)", err)
	}
	var r struct {
		ProgramID string `json:"program_id"`
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return solrpc.PublicKey{}, err
	}
	return solrpc.ParseKey(r.ProgramID)
}

func number(s string) uint64 {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		log.Fatalf("expected a number, got %q", s)
	}
	return n
}
