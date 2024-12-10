package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"log"
	"math/big"
	"net/http"
	"time"
)

const (
	infuraURL   = "https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191"
	firebaseURL = "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app"
)

// Структуры для записи данных в Firebase (без изменений)
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
	From     string `json:"from"`
}

// Запись данных о блоке в Firebase
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

// Запись транзакций в Firebase
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

func main() {
	ctx := context.Background()

	// Подключение к Ethereum-ноде через Infura
	client, err := ethclient.DialContext(ctx, infuraURL)
	if err != nil {
		log.Fatalf("Error connecting to Ethereum node: %v", err)
	}

	// Получение chainID для определения Signer
	chainID, err := client.NetworkID(ctx)
	if err != nil {
		log.Fatalf("Error getting network ID: %v", err)
	}
	signer := types.LatestSignerForChainID(chainID)

	// Получение последнего блока
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		log.Fatalf("Error getting latest block header: %v", err)
	}
	currentBlock := header.Number.Uint64()

	for {
		latestHeader, err := client.HeaderByNumber(ctx, nil)
		if err != nil {
			log.Println("Error getting latest block header:", err)
			time.Sleep(15 * time.Second)
			continue
		}

		newLatestBlock := latestHeader.Number.Uint64()
		if newLatestBlock > currentBlock {
			for bNum := currentBlock + 1; bNum <= newLatestBlock; bNum++ {
				bigNum := big.NewInt(int64(bNum))
				block, err := client.BlockByNumber(ctx, bigNum)
				if err != nil {
					log.Println("Error fetching block:", err)
					continue
				}

				// Получение данных блока
				blockNumber := block.NumberU64()
				blockTime := block.Time()
				blockDifficulty := block.Difficulty().Uint64()
				blockHash := block.Hash().Hex()
				txCount := len(block.Transactions())

				// Подготовка данных для записи в Firebase
				bData := BlockData{
					Number:     blockNumber,
					Time:       blockTime,
					Difficulty: blockDifficulty,
					Hash:       blockHash,
					TxCount:    txCount,
				}

				// Запись блока в Firebase
				if err := writeBlockToFirebase(bData); err != nil {
					log.Println("Error writing block data to Firebase:", err)
				} else {
					fmt.Println("Block data written to Firebase for block:", bData.Number)
				}

				// Подготовка данных транзакций
				var txs []TransactionData
				for _, tx := range block.Transactions() {
					msg, err := tx.AsMessage(signer, block.BaseFee())
					if err != nil {
						// Если не удалось получить сообщение (например, проблемы с сигнатурой), пропускаем
						continue
					}

					value := tx.Value()
					gas := tx.Gas()
					gasPrice := tx.GasPrice()
					to := ""
					if tx.To() != nil {
						to = tx.To().Hex()
					}

					txData := TransactionData{
						Hash:     tx.Hash().Hex(),
						Value:    value.String(),
						To:       to,
						Gas:      gas,
						GasPrice: gasPrice.String(),
						From:     msg.From().Hex(),
					}
					txs = append(txs, txData)
				}

				// Запись транзакций в Firebase
				if err := writeTransactionsToFirebase(blockNumber, txs); err != nil {
					log.Println("Error writing transactions to Firebase:", err)
				} else {
					fmt.Println("Transactions data written to Firebase for block:", blockNumber)
				}
			}
			currentBlock = newLatestBlock
		}

		// Пауза перед следующей проверкой
		time.Sleep(15 * time.Second)
	}
}
