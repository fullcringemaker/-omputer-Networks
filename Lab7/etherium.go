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

// Данный код:
// 1. Очищает таблицу в Firebase перед началом работы (DELETE запрос).
// 2. Подключается к Ethereum ноде через Infura.
// 3. Использует подписку на новые заголовки (SubscribeNewHead) для мгновенной реакции на появление новых блоков.
// 4. При появлении нового блока загружает весь блок и транзакции в Firebase.
// 5. В консоль выводит только состояние процесса поиска и подтверждения, что новый блок найден, без деталей блока или транзакций.
//    Никакой другой информации в консоль не выводится.
// 6. Блоки должны идти подряд, не пропуская промежуточных.

func main() {
	// 1. Перед началом записи данных отправляем запрос DELETE к Firebase, чтобы очистить таблицу.
	firebaseURL := "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/.json"
	req, err := http.NewRequest("DELETE", firebaseURL, nil)
	if err != nil {
		log.Fatal("Ошибка при создании запроса на очистку Firebase:", err)
	}
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatal("Ошибка при очистке Firebase:", err)
	}
	_ = resp.Body.Close()

	// 2. Подключение к Ethereum клиенту через Infura
	infuraURL := "https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191"
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalln(err)
	}

	// 3. Подписываемся на новые заголовки блоков
	headersChan := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(context.Background(), headersChan)
	if err != nil {
		log.Fatal("Ошибка при подписке на новые заголовки:", err)
	}

	// Выводим минимальную информацию о процессе поиска блоков
	fmt.Println("Ожидание новых блоков...")

	// Цикл чтения новых заголовков из канала
	for {
		select {
		case err := <-sub.Err():
			// Если произошла ошибка в подписке, делаем паузу и пробуем переподключиться.
			fmt.Println("Ошибка подписки, повторное подключение...")
			time.Sleep(time.Second * 5)
			sub, err = client.SubscribeNewHead(context.Background(), headersChan)
			if err != nil {
				fmt.Println("Не удалось переподключиться к подписке, ждем и повторяем...")
				time.Sleep(time.Second * 5)
				continue
			}
			fmt.Println("Переподключение к подписке выполнено. Ожидание новых блоков...")

		case header := <-headersChan:
			// Получили новый заголовок, выводим только что мы нашли новый блок:
			fmt.Printf("Найден новый блок: %s\n", header.Number.String())

			// Получаем сам блок по номеру
			blockNumber := header.Number
			block, err := client.BlockByNumber(context.Background(), blockNumber)
			if err != nil {
				log.Println("Ошибка при получении блока:", err)
				continue
			}

			// Подготавливаем данные для Firebase
			blockData := map[string]interface{}{
				"number":            block.Number().Uint64(),
				"time":              block.Time(),
				"difficulty":        block.Difficulty().Uint64(),
				"hash":              block.Hash().Hex(),
				"transactionsCount": len(block.Transactions()),
			}

			// Записываем данные блока в Firebase
			blockUrl := fmt.Sprintf("https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/blocks/%d.json", block.Number().Uint64())
			blockDataBytes, _ := json.Marshal(blockData)
			reqBlock, _ := http.NewRequest("PUT", blockUrl, bytes.NewBuffer(blockDataBytes))
			reqBlock.Header.Set("Content-Type", "application/json")
			respBlock, err := httpClient.Do(reqBlock)
			if err != nil {
				log.Println("Ошибка при записи блока в Firebase:", err)
				continue
			}
			_ = respBlock.Body.Close()

			// Записываем транзакции блока в Firebase
			for i, tx := range block.Transactions() {
				toAddr := tx.To()
				var toStr string
				if toAddr != nil {
					toStr = toAddr.Hex()
				} else {
					toStr = "nil"
				}

				txData := map[string]interface{}{
					"chainId":  tx.ChainId().String(),
					"hash":     tx.Hash().Hex(),
					"value":    tx.Value().String(),
					"cost":     tx.Cost().String(),
					"to":       toStr,
					"gas":      tx.Gas(),
					"gasPrice": tx.GasPrice().String(),
				}

				txUrl := fmt.Sprintf("https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/blocks/%d/transactions/%d.json", block.Number().Uint64(), i)
				txDataBytes, _ := json.Marshal(txData)
				reqTx, _ := http.NewRequest("PUT", txUrl, bytes.NewBuffer(txDataBytes))
				reqTx.Header.Set("Content-Type", "application/json")
				respTx, err := httpClient.Do(reqTx)
				if err != nil {
					log.Println("Ошибка при записи транзакции в Firebase:", err)
					continue
				}
				_ = respTx.Body.Close()
			}
		}
	}
}
