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

type TransactionData struct {
	ChainId  string `json:"chainId"`
	Hash     string `json:"hash"`
	Value    string `json:"value"`
	Cost     string `json:"cost"`
	To       string `json:"to,omitempty"`
	Gas      string `json:"gas"`
	GasPrice string `json:"gasPrice"`
}

type BlockData struct {
	Number       uint64           `json:"number"`
	Time         uint64           `json:"time"`
	Difficulty   uint64           `json:"difficulty"`
	Hash         string           `json:"hash"`
	Transactions []TransactionData `json:"transactions"`
}

func main() {
	// Подключение к инфура
	client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
	if err != nil {
		log.Fatalln("Ошибка подключения к инфура:", err)
	}

	// Получаем последний блок
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal("Ошибка получения заголовка последнего блока:", err)
	}
	blockNumber := big.NewInt(header.Number.Int64())
	block, err := client.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		log.Fatal("Ошибка получения блока по номеру:", err)
	}

	// Формируем данные блока
	var transactionsData []TransactionData
	for _, tx := range block.Transactions() {
		toAddr := ""
		if tx.To() != nil {
			toAddr = tx.To().String()
		}

		txData := TransactionData{
			ChainId:  tx.ChainId().String(),
			Hash:     tx.Hash().String(),
			Value:    tx.Value().String(),
			Cost:     tx.Cost().String(),
			To:       toAddr,
			Gas:      fmt.Sprintf("%d", tx.Gas()),
			GasPrice: tx.GasPrice().String(),
		}
		transactionsData = append(transactionsData, txData)
	}

	blockData := BlockData{
		Number:       block.NumberU64(),
		Time:         block.Time(),
		Difficulty:   block.Difficulty().Uint64(),
		Hash:         block.Hash().Hex(),
		Transactions: transactionsData,
	}

	// Подготовка данных для отправки в Firebase Realtime Database
	jsonData, err := json.Marshal(blockData)
	if err != nil {
		log.Fatal("Ошибка при сериализации данных блока в JSON:", err)
	}

	// Отправка данных о блоке в Firebase
	// Формируем путь: .../blocks/<номер_блока>.json
	firebaseURL := fmt.Sprintf("https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/blocks/%d.json", blockData.Number)
	req, err := http.NewRequest("PUT", firebaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal("Ошибка при формировании запроса к Firebase:", err)
	}

	req.Header.Set("Content-Type", "application/json")
	clientHttp := &http.Client{Timeout: 10 * time.Second}
	resp, err := clientHttp.Do(req)
	if err != nil {
		log.Fatal("Ошибка при отправке запроса к Firebase:", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Fatalf("Неудачный запрос к Firebase, код ответа: %d\n", resp.StatusCode)
	}

	fmt.Println("Данные о блоке и транзакциях успешно отправлены в Firebase!")
}
