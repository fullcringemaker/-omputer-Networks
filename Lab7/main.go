package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	infuraURL   = "https://mainnet.infura.io/v3/8133ff0c11dc491daac3f680d2f74d18"
	firebaseURL = "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app"
)

type BlockData struct {
	Number     uint64 `json:"number"`
	Time       uint64 `json:"time"`
	Difficulty uint64 `json:"difficulty"`
	Hash       string `json:"hash"`
	TxCount    int    `json:"txCount"`
}
type TransactionData struct {
	Hash     string `json:"hash"`
	Value    string `json:"value"`
	To       string `json:"to"`
	Gas      uint64 `json:"gas"`
	GasPrice string `json:"gasPrice"`
	Cost     string `json:"cost"`
	ChainId  string `json:"chainId"`
}

func writeBlockToFirebase(blockData BlockData) error {
	url := fmt.Sprintf("%s/blocks/%d.json", firebaseURL, blockData.Number)
	bodyBytes, err := json.Marshal(blockData)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	clientHttp := &http.Client{}
	resp, err := clientHttp.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("failed to write block data to firebase: status code %d", resp.StatusCode)
	}
	return nil
}
func writeTransactionsToFirebase(blockNumber uint64, txs []TransactionData) error {
	url := fmt.Sprintf("%s/blocks/%d/transactions.json", firebaseURL, blockNumber)
	bodyBytes, err := json.Marshal(txs)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	clientHttp := &http.Client{}
	resp, err := clientHttp.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("failed to write transactions data to firebase: status code %d", resp.StatusCode)
	}
	return nil
}
func clearFirebase() error {
	url := fmt.Sprintf("%s/blocks.json", firebaseURL)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	clientHttp := &http.Client{}
	resp, err := clientHttp.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("failed to clear firebase data: status code %d", resp.StatusCode)
	}
	return nil
}
func exampleGetLatestBlock() {
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalln(err)
	}
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(header.Number.String())
	blockNumber := big.NewInt(header.Number.Int64())
	block, err := client.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(block.Number().Uint64())
	fmt.Println(block.Time())
	fmt.Println(block.Difficulty().Uint64())
	fmt.Println(block.Hash().Hex())
	fmt.Println(len(block.Transactions()))
}
func exampleGetBlockByNumber() {
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalln(err)
	}
	blockNumber := big.NewInt(15960495)
	block, err := client.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(block.Number().Uint64())
	fmt.Println(block.Time())
	fmt.Println(block.Difficulty().Uint64())
	fmt.Println(block.Hash().Hex())
	fmt.Println(len(block.Transactions()))
}
func exampleGetTransactionData() {
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalln(err)
	}
	blockNumber := big.NewInt(15960495)
	block, err := client.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		log.Fatal(err)
	}
	for _, tx := range block.Transactions() {
		fmt.Println(tx.ChainId())
		fmt.Println(tx.Hash())
		fmt.Println(tx.Value())
		fmt.Println(tx.Cost())
		fmt.Println(tx.To())
		fmt.Println(tx.Gas())
		fmt.Println(tx.GasPrice())
	}
}
func main() {
	if err := clearFirebase(); err != nil {
		log.Println("Error clearing Firebase data:", err)
	} else {
		fmt.Println("Firebase data cleared successfully")
	}
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalln("Error connecting to Infura:", err)
	}
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatalln("Error getting latest block header:", err)
	}
	latestBlock := header.Number.Int64()
	currentBlock := latestBlock
	for {
		newHeader, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			log.Println("Error getting latest block number:", err)
			time.Sleep(15 * time.Second)
			continue
		}
		newLatestBlock := newHeader.Number.Int64()
		if newLatestBlock > currentBlock {
			for bNum := currentBlock + 1; bNum <= newLatestBlock; bNum++ {
				block, err := client.BlockByNumber(context.Background(), big.NewInt(bNum))
				if err != nil {
					log.Println("Error fetching block:", err)
					continue
				}
				bData := BlockData{
					Number:     block.Number().Uint64(),
					Time:       block.Time(),
					Difficulty: block.Difficulty().Uint64(),
					Hash:       block.Hash().Hex(),
					TxCount:    len(block.Transactions()),
				}
				if err := writeBlockToFirebase(bData); err != nil {
					log.Println("Error writing block data to Firebase:", err)
				} else {
					fmt.Println("Block data written to Firebase for block:", bData.Number)
				}
				var txs []TransactionData
				for _, tx := range block.Transactions() {
					gas := tx.Gas()
					value := tx.Value().String()
					gasPrice := tx.GasPrice().String()
					cost := tx.Cost().String()
					chainId := ""
					if tx.ChainId() != nil {
						chainId = tx.ChainId().String()
					}
					toAddress := ""
					if tx.To() != nil {
						toAddress = tx.To().Hex()
					}
					txData := TransactionData{
						Hash:     tx.Hash().Hex(),
						Value:    value,
						To:       toAddress,
						Gas:      gas,
						GasPrice: gasPrice,
						Cost:     cost,
						ChainId:  chainId,
					}
					txs = append(txs, txData)
				}
				if err := writeTransactionsToFirebase(block.Number().Uint64(), txs); err != nil {
					log.Println("Error writing transactions to Firebase:", err)
				} else {
					fmt.Println("Transactions data written to Firebase for block:", block.Number().Uint64())
				}
			}
			currentBlock = newLatestBlock
		}
		time.Sleep(5 * time.Second)
	}
}
