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

// Данная структура будет использоваться для записи транзакций в Firebase
type TransactionData struct {
	ChainID   string `json:"chainId"`
	Hash      string `json:"hash"`
	Value     string `json:"value"`
	Cost      string `json:"cost"`
	To        string `json:"to"`
	Gas       uint64 `json:"gas"`
	GasPrice  string `json:"gasPrice"`
}

// Данная структура будет использоваться для записи блока в Firebase
type BlockData struct {
	Number       uint64            `json:"number"`
	Time         uint64            `json:"time"`
	Difficulty   uint64            `json:"difficulty"`
	Hash         string            `json:"hash"`
	TxCount      int               `json:"txCount"`
	Transactions []TransactionData `json:"transactions"`
}

func main() {
	// Ниже приведены исходные коды, которые должны присутствовать в итоговом коде.
	// В них заменены данные на актуальные значения, предоставленные в задании.
	// В итоговой программе будет использоваться один и тот же клиент, но мы продемонстрируем изначальный код.

	// -------------------------------------
	// Получение последнего блока (пример кода из задания с заменой данных)
	{
		client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
		if err != nil {
			log.Fatalln(err)
		}
		header, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(header.Number.String()) // Вывод номера последнего блока
		blockNumber := big.NewInt(header.Number.Int64())
		block, err := client.BlockByNumber(context.Background(), blockNumber)
		if err != nil {
			log.Fatal(err)
		}
		// Вывод данных о блоке
		fmt.Println(block.Number().Uint64())
		fmt.Println(block.Time())
		fmt.Println(block.Difficulty().Uint64())
		fmt.Println(block.Hash().Hex())
		fmt.Println(len(block.Transactions()))
	}

	// -------------------------------------
	// Получение данных из блока по номеру (пример кода из задания с заменой данных)
	{
		client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
		if err != nil {
			log.Fatalln(err)
		}
		blockNumber := big.NewInt(15960495)
		block, err := client.BlockByNumber(context.Background(), blockNumber)
		if err != nil {
			log.Fatal(err)
		}
		// Вывод данных о блоке
		fmt.Println(block.Number().Uint64())
		fmt.Println(block.Time())
		fmt.Println(block.Difficulty().Uint64())
		fmt.Println(block.Hash().Hex())
		fmt.Println(len(block.Transactions()))
	}

	// -------------------------------------
	// Получение данных из полей транзакции (пример кода из задания с заменой данных)
	{
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

	// -------------------------------------
	// Ниже основная логика приложения, реализующая мониторинг новых блоков Ethereum и запись их в Firebase.
	//
	// Мы будем периодически проверять появление нового блока в сети Ethereum (через Infura),
	// получать все транзакции блока и записывать их в Firebase Realtime Database.
	//
	// Firebase будет обновляться в режиме реального времени. Предполагается, что настройки Firebase позволяют запись без авторизации.
	//
	// При записи в Firebase мы будем использовать REST API Realtime Database.
	//
	// Ссылка для Firebase Realtime Database (предоставлена в задании):
	// https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/
	//
	// Мы будем записывать данные о блоках и транзакциях в путь:
	// blocks/{blockNumber}/transactions.json
	//
	// Таким образом, с каждым новым блоком мы будем добавлять новые данные, которые сразу появятся у всех клиентов, подписанных на эти данные.

	// Создадим подключение к клиенту Infura
	client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
	if err != nil {
		log.Fatalln(err)
	}

	// Определим функцию записи данных о блоке и транзакциях в Firebase
	writeBlockToFirebase := func(block *BlockData) error {
		// Преобразуем данные в JSON
		jsonData, err := json.Marshal(block)
		if err != nil {
			return err
		}

		// Пишем данные о блоке: blocks/{blockNumber}.json
		url := fmt.Sprintf("https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/blocks/%d.json", block.Number)

		req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("failed to write block data to firebase, status: %s", resp.Status)
		}
		return nil
	}

	// Получаем номер последнего блока для начала
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	lastBlock := header.Number.Uint64()

	// Запустим цикл, который будет проверять новые блоки каждые ~15 секунд.
	// При появлении нового блока мы будем его считывать и записывать в Firebase.
	for {
		time.Sleep(15 * time.Second)

		header, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			log.Println("Error fetching latest block header:", err)
			continue
		}
		currentBlock := header.Number.Uint64()

		if currentBlock > lastBlock {
			// Есть новые блоки
			for bNum := lastBlock + 1; bNum <= currentBlock; bNum++ {
				blockNumber := big.NewInt(int64(bNum))
				block, err := client.BlockByNumber(context.Background(), blockNumber)
				if err != nil {
					log.Println("Error fetching block:", err)
					continue
				}

				// Подготовим данные для записи в Firebase
				var txData []TransactionData
				for _, tx := range block.Transactions() {
					to := ""
					if tx.To() != nil {
						to = tx.To().Hex()
					}
					txData = append(txData, TransactionData{
						ChainID:  tx.ChainId().String(),
						Hash:     tx.Hash().Hex(),
						Value:    tx.Value().String(),
						Cost:     tx.Cost().String(),
						To:       to,
						Gas:      tx.Gas(),
						GasPrice: tx.GasPrice().String(),
					})
				}

				blockData := &BlockData{
					Number:       block.Number().Uint64(),
					Time:         block.Time(),
					Difficulty:   block.Difficulty().Uint64(),
					Hash:         block.Hash().Hex(),
					TxCount:      len(block.Transactions()),
					Transactions: txData,
				}

				err = writeBlockToFirebase(blockData)
				if err != nil {
					log.Println("Error writing block to firebase:", err)
				} else {
					log.Printf("Successfully wrote block %d to firebase\n", bNum)
				}
			}
			lastBlock = currentBlock
		}
	}
}
