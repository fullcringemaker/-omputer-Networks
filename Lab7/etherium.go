package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
	"log"
	"math/big"
	"net/http"
	"time"
)

// Структуры для записи в Firebase
type BlockData struct {
	Number     uint64   `json:"number"`
	Time       uint64   `json:"time"`
	Difficulty uint64   `json:"difficulty"`
	Hash       string   `json:"hash"`
	TxCount    int      `json:"txCount"`
}

type TransactionData struct {
	ChainId  string `json:"chainId"`
	Hash     string `json:"hash"`
	Value    string `json:"value"`
	Cost     string `json:"cost"`
	To       string `json:"to"`
	Gas      uint64 `json:"gas"`
	GasPrice string `json:"gasPrice"`
}

// Функция для записи блока в Firebase
func writeBlockToFirebase(blockData BlockData, firebaseURL string) error {
	// Записываем данные блока по адресу: /blocks/<номер_блока>.json
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

// Функция для записи транзакций в Firebase
func writeTransactionsToFirebase(blockNumber uint64, txs []TransactionData, firebaseURL string) error {
	// Записываем данные транзакций по адресу: /blocks/<номер_блока>/transactions.json
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
	// Подключение к инфуре
	infuraURL := "https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191"
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalln(err)
	}

	// URL Firebase Realtime Database (предоставлен)
	firebaseURL := "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app"

	ctx := context.Background()

	// Начнем с получения последнего блока
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	currentBlock := header.Number.Int64()

	// Будем в цикле отслеживать новые блоки, и при появлении нового блока будем писать данные о блоке и транзакциях в Firebase
	// Для демонстрации - бесконечный цикл с проверкой новых блоков примерно каждые 15 секунд
	// В реальном применении можно использовать подписки, но в примере - простой polling

	for {
		latestHeader, err := client.HeaderByNumber(ctx, nil)
		if err != nil {
			log.Println("Error getting latest header:", err)
			time.Sleep(15 * time.Second)
			continue
		}

		latestBlockNumber := latestHeader.Number.Int64()

		if latestBlockNumber > currentBlock {
			// Есть новые блоки, обрабатываем их
			for blockNum := currentBlock + 1; blockNum <= latestBlockNumber; blockNum++ {
				block, err := client.BlockByNumber(ctx, big.NewInt(blockNum))
				if err != nil {
					log.Println("Error fetching block:", err)
					continue
				}

				// Собираем данные о блоке
				bData := BlockData{
					Number:     block.NumberU64(),
					Time:       block.Time(),
					Difficulty: block.Difficulty().Uint64(),
					Hash:       block.Hash().Hex(),
					TxCount:    len(block.Transactions()),
				}

				// Пишем данные блока в Firebase
				err = writeBlockToFirebase(bData, firebaseURL)
				if err != nil {
					log.Println("Error writing block data to Firebase:", err)
				} else {
					fmt.Println("Block data written to Firebase for block:", bData.Number)
				}

				// Теперь собираем данные по транзакциям
				var txs []TransactionData
				for _, tx := range block.Transactions() {
					txData := TransactionData{
						ChainId:  tx.ChainId().String(),
						Hash:     tx.Hash().Hex(),
						Value:    tx.Value().String(),
						Cost:     tx.Cost().String(),
						Gas:      tx.Gas(),
						GasPrice: tx.GasPrice().String(),
					}
					// Поле To может быть nil (например, если это контрактная транзакция)
					if tx.To() != nil {
						txData.To = tx.To().Hex()
					} else {
						txData.To = "null"
					}
					txs = append(txs, txData)
				}

				// Пишем транзакции в Firebase
				err = writeTransactionsToFirebase(block.NumberU64(), txs, firebaseURL)
				if err != nil {
					log.Println("Error writing transactions to Firebase:", err)
				} else {
					fmt.Println("Transactions data written to Firebase for block:", block.NumberU64())
				}
			}

			// Обновляем текущий обработанный блок
			currentBlock = latestBlockNumber
		}

		// Подождём перед следующей проверкой
		time.Sleep(15 * time.Second)
	}
}

