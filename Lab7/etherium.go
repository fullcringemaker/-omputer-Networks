package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"time"

	"firebase.google.com/go"
	"firebase.google.com/go/db"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"google.golang.org/api/option"
)

const (
	infuraURL  = "https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191"
	firebaseDB = "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/"
)

type BlockData struct {
	Number       uint64           `json:"number"`
	Time         uint64           `json:"time"`
	Difficulty   uint64           `json:"difficulty"`
	Hash         string           `json:"hash"`
	Transactions []TransactionData `json:"transactions"`
}

type TransactionData struct {
	ChainID   *big.Int `json:"chainId"`
	Hash      string   `json:"hash"`
	Value     *big.Int `json:"value"`
	Cost      *big.Int `json:"cost"`
	To        string   `json:"to"`
	Gas       uint64   `json:"gas"`
	GasPrice  *big.Int `json:"gasPrice"`
}

func main() {
	// Connect to Ethereum client
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalf("Failed to connect to Ethereum client: %v", err)
	}

	// Firebase setup
	opt := option.WithoutAuthentication()
	firebaseApp, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase app: %v", err)
	}

	firebaseClient, err := firebaseApp.Database(context.Background())
	if err != nil {
		log.Fatalf("Failed to initialize Firebase Database client: %v", err)
	}

	// Monitor Ethereum blocks
	for {
		err := monitorLatestBlock(client, firebaseClient)
		if err != nil {
			log.Printf("Error monitoring block: %v", err)
		}
		time.Sleep(10 * time.Second) // Adjust interval as needed
	}
}

func monitorLatestBlock(client *ethclient.Client, firebaseClient *db.Client) error {
	// Get latest block header
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to get latest block header: %w", err)
	}

	blockNumber := big.NewInt(header.Number.Int64())
	block, err := client.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		return fmt.Errorf("failed to get block by number: %w", err)
	}

	// Extract block data
	blockData := BlockData{
		Number:     block.Number().Uint64(),
		Time:       block.Time(),
		Difficulty: block.Difficulty().Uint64(),
		Hash:       block.Hash().Hex(),
	}

	for _, tx := range block.Transactions() {
		txData := TransactionData{
			ChainID:  tx.ChainId(),
			Hash:     tx.Hash().Hex(),
			Value:    tx.Value(),
			Cost:     tx.Cost(),
			To:       toAddressString(tx.To()),
			Gas:      tx.Gas(),
			GasPrice: tx.GasPrice(),
		}
		blockData.Transactions = append(blockData.Transactions, txData)
	}

	// Save to Firebase
	ref := firebaseClient.NewRef(fmt.Sprintf("blocks/%d", blockData.Number))
	if err := ref.Set(context.Background(), blockData); err != nil {
		return fmt.Errorf("failed to write block data to Firebase: %w", err)
	}

	log.Printf("Block %d saved to Firebase", blockData.Number)
	return nil
}

func toAddressString(addr *types.Address) string {
	if addr == nil {
		return "nil"
	}
	return addr.Hex()
}
