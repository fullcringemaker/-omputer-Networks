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
	// Подключение к infura
	client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
	if err != nil {
		log.Fatalln("Ошибка подключения к инфура:", err)
	}

	fmt.Println("Программа запущена. Ожидание новых блоков...")

	var lastProcessedBlock *big.Int

	// Бесконечный цикл, пока пользователь не остановит программу
	for {
		// Получаем последний блок
		header, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			log.Println("Ошибка получения заголовка последнего блока:", err)
			time.Sleep(5 * time.Second)
			continue
		}

		currentBlockNumber := header.Number
		// Если мы еще не обрабатывали блоки или текущий блок больше последнего обработанного
		if lastProcessedBlock == nil || currentBlockNumber.Cmp(lastProcessedBlock) == 1 {
			fmt.Printf("Найден новый блок. Нужен блок номер %d\n", currentBlockNumber.Uint64())

			block, err := client.BlockByNumber(context.Background(), currentBlockNumber)
			if err != nil {
				log.Println("Ошибка получения блока по номеру:", err)
				time.Sleep(5 * time.Second)
				continue
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

			fmt.Printf("Получен блок %d. Отправка данных в Firebase...\n", blockData.Number)

			// Подготовка данных для отправки в Firebase Realtime Database
			jsonData, err := json.Marshal(blockData)
			if err != nil {
				log.Println("Ошибка при сериализации данных блока в JSON:", err)
				time.Sleep(5 * time.Second)
				continue
			}

			// Отправка данных о блоке в Firebase
			firebaseURL := fmt.Sprintf("https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/blocks/%d.json", blockData.Number)
			req, err := http.NewRequest("PUT", firebaseURL, bytes.NewBuffer(jsonData))
			if err != nil {
				log.Println("Ошибка при формировании запроса к Firebase:", err)
				time.Sleep(5 * time.Second)
				continue
			}

			req.Header.Set("Content-Type", "application/json")
			clientHttp := &http.Client{Timeout: 10 * time.Second}
			resp, err := clientHttp.Do(req)
			if err != nil {
				log.Println("Ошибка при отправке запроса к Firebase:", err)
				time.Sleep(5 * time.Second)
				continue
			}
			resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				log.Printf("Неудачный запрос к Firebase, код ответа: %d\n", resp.StatusCode)
				time.Sleep(5 * time.Second)
				continue
			}

			fmt.Printf("Данные о блоке %d и транзакциях успешно отправлены в Firebase!\n", blockData.Number)

			// Обновляем последний обработанный блок
			lastProcessedBlock = currentBlockNumber
		} else {
			// Если новый блок не найден, ждем
			fmt.Println("Новых блоков нет. Ожидание...")
		}

		// Делаем небольшую задержку между проверками
		time.Sleep(5 * time.Second)
	}
}
