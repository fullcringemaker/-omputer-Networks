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

// ВНИМАНИЕ: Данный код объединяет все указанные требования в один файл/один код.
// Он подключается к Ethereum blockchain через infura.io, получает блоки и транзакции,
// записывает данные в Firebase Realtime Database, очищая базу данных при каждом запуске.
// При появлении нового блока данные автоматически обновляются в Firebase,
// следовательно, на клиентской стороне (при подключении к Firebase) данные обновятся
// без перезагрузки страницы.

// Исходные фрагменты кода, которые должны быть включены в итоговый код (адаптированы под новые данные):
// ----- Получение последнего блока (адаптирован) -----
// Пример кода: получение последнего блока и вывод его информации
// (Этот код присутствует в итоговом решении, но с заменой адреса infura)
func exampleGetLastBlock() {
	client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
	if err != nil {
		log.Fatalln(err)
	}
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(header.Number.String())
	blockNumber := big.NewInt(header.Number.Int64())
	block, err := client.BlockByNumber(context.Background(), blockNumber) //get block with this number
	if err != nil {
		log.Fatal(err)
	}
	// all info about block
	fmt.Println(block.Number().Uint64())
	fmt.Println(block.Time())
	fmt.Println(block.Difficulty().Uint64())
	fmt.Println(block.Hash().Hex())
	fmt.Println(len(block.Transactions()))
}

// ----- Получение данных из блока по номеру (адаптирован) -----
// Пример кода: получение данных блока по заданному номеру
// (В примере был жестко прописан номер блока и другой ключ, мы используем тот же URL infura, но можем взять любой блок)
func exampleGetBlockByNumber() {
	client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
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

// ----- Получение данных из полей транзакции (адаптирован) -----
// Пример кода: получение транзакций для указанного блока и вывод их полей
func exampleGetTransactionsFromBlock() {
	client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
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

// ------------------------------------
// Основная реализация программы:
// ------------------------------------

func main() {

	// 1. Перед началом записи данных отправляем запрос DELETE к Firebase, чтобы очистить таблицу.
	firebaseURL := "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/.json"
	req, err := http.NewRequest("DELETE", firebaseURL, nil)
	if err != nil {
		log.Fatal("Error creating request to Firebase:", err)
	}
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatal("Error deleting Firebase data:", err)
	}
	_ = resp.Body.Close()

	// 2. Подключение к Ethereum клиенту через Infura
	infuraURL := "https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191"
	client, err := ethclient.Dial(infuraURL)
	if err != nil {
		log.Fatalln(err)
	}

	// Выполним примеры, чтобы код из примеров тоже присутствовал (можно вызвать в начале, чтобы продемонстрировать)
	exampleGetLastBlock()
	exampleGetBlockByNumber()
	exampleGetTransactionsFromBlock()

	// 3. Подписываемся на новые заголовки блоков, чтобы получать новые блоки в реальном времени
	headers := make(chan *big.Int)
	go func() {
		// Эта горутина будет опрашивать последние блоки вручную (polling), чтобы упростить логику.
		// Альтернативно можно использовать SubscribeNewHead, но иногда проще polling:
		var lastBlockNumber *big.Int

		for {
			header, err := client.HeaderByNumber(context.Background(), nil)
			if err != nil {
				log.Println("Error getting latest header:", err)
				time.Sleep(5 * time.Second)
				continue
			}
			if lastBlockNumber == nil || header.Number.Cmp(lastBlockNumber) > 0 {
				// Новый блок найден
				headers <- header.Number
				lastBlockNumber = header.Number
			}
			time.Sleep(5 * time.Second) // Пауза между проверками (для реального продакшена можно использовать SubscribeNewHead)
		}
	}()

	for {
		select {
		case blockNumber := <-headers:
			// Получаем данные блока по номеру
			block, err := client.BlockByNumber(context.Background(), blockNumber)
			if err != nil {
				log.Fatal("Error getting block:", err)
			}

			// Выводим данные блока в консоль (как в примерах)
			fmt.Println("New Block:", block.Number().Uint64())
			fmt.Println("Time:", block.Time())
			fmt.Println("Difficulty:", block.Difficulty().Uint64())
			fmt.Println("Hash:", block.Hash().Hex())
			fmt.Println("Transactions count:", len(block.Transactions()))

			// Подготавливаем данные блока для записи в Firebase
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
				log.Fatal("Error writing block to Firebase:", err)
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
					log.Fatal("Error writing transaction to Firebase:", err)
				}
				_ = respTx.Body.Close()
			}

			// Теперь данные о новом блоке и транзакциях записаны в Firebase Realtime Database.
			// При подключении к этой БД с помощью Firebase SDK на странице веб-приложения,
			// новые данные будут появляться в реальном времени без перезагрузки страницы.
		}
	}
}
