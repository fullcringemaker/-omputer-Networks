package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
	"log"
	"math/big"
	"net/http"
	"bytes"
	"time"
)

// Структура для записи данных блока
type BlockData struct {
	Number       uint64         `json:"number"`
	Time         uint64         `json:"time"`
	Difficulty   uint64         `json:"difficulty"`
	Hash         string         `json:"hash"`
	Transactions []Transaction  `json:"transactions"`
}

// Структура для записи данных транзакции
type Transaction struct {
	ChainId  string `json:"chain_id"`
	Hash     string `json:"hash"`
	Value    string `json:"value"`
	Cost     string `json:"cost"`
	To       string `json:"to"`
	Gas      uint64 `json:"gas"`
	GasPrice string `json:"gas_price"`
}

func main() {
	// Подключаемся к Infura с указанным ключом
	client, err := ethclient.Dial("https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191")
	if err != nil {
		log.Fatalln(err)
	}

	// URL вашей Firebase Realtime Database
	firebaseURL := "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app"

	// Получаем последний блок
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Последний блок (номер):", header.Number.String())

	// Получаем сам последний блок
	blockNumber := big.NewInt(header.Number.Int64())
	block, err := client.BlockByNumber(context.Background(), blockNumber)
	if err != nil {
		log.Fatal(err)
	}

	// Выводим информацию о блоке
	fmt.Println("Block Number:", block.Number().Uint64())
	fmt.Println("Block Time:", block.Time())
	fmt.Println("Block Difficulty:", block.Difficulty().Uint64())
	fmt.Println("Block Hash:", block.Hash().Hex())
	fmt.Println("Transactions Count:", len(block.Transactions()))

	// Получаем данные транзакций из данного блока
	var transactions []Transaction
	for _, tx := range block.Transactions() {
		toAddr := ""
		if tx.To() != nil {
			toAddr = tx.To().Hex()
		}

		t := Transaction{
			ChainId:  tx.ChainId().String(),
			Hash:     tx.Hash().Hex(),
			Value:    tx.Value().String(),
			Cost:     tx.Cost().String(),
			To:       toAddr,
			Gas:      tx.Gas(),
			GasPrice: tx.GasPrice().String(),
		}
		transactions = append(transactions, t)
	}

	// Формируем структуру для записи в Firebase
	blockData := BlockData{
		Number:       block.Number().Uint64(),
		Time:         block.Time(),
		Difficulty:   block.Difficulty().Uint64(),
		Hash:         block.Hash().Hex(),
		Transactions: transactions,
	}

	// Записываем данные о текущем блоке и его транзакциях в Firebase
	err = writeToFirebase(firebaseURL, blockData)
	if err != nil {
		log.Fatalf("Ошибка записи в Firebase: %v", err)
	}
	fmt.Println("Данные последнего блока записаны в Firebase.")

	// Дополнительно продемонстрируем получение данных о другом блоке по номеру (пример: 15960495)
	anotherBlockNum := big.NewInt(15960495)
	anotherBlock, err := client.BlockByNumber(context.Background(), anotherBlockNum)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Данные о блоке 15960495:")
	fmt.Println("Block Number:", anotherBlock.Number().Uint64())
	fmt.Println("Block Time:", anotherBlock.Time())
	fmt.Println("Block Difficulty:", anotherBlock.Difficulty().Uint64())
	fmt.Println("Block Hash:", anotherBlock.Hash().Hex())
	fmt.Println("Transactions Count:", len(anotherBlock.Transactions()))

	// Получение транзакций из блока 15960495
	var anotherBlockTransactions []Transaction
	for _, tx := range anotherBlock.Transactions() {
		toAddr := ""
		if tx.To() != nil {
			toAddr = tx.To().Hex()
		}

		t := Transaction{
			ChainId:  tx.ChainId().String(),
			Hash:     tx.Hash().Hex(),
			Value:    tx.Value().String(),
			Cost:     tx.Cost().String(),
			To:       toAddr,
			Gas:      tx.Gas(),
			GasPrice: tx.GasPrice().String(),
		}
		anotherBlockTransactions = append(anotherBlockTransactions, t)
	}

	anotherBlockData := BlockData{
		Number:       anotherBlock.Number().Uint64(),
		Time:         anotherBlock.Time(),
		Difficulty:   anotherBlock.Difficulty().Uint64(),
		Hash:         anotherBlock.Hash().Hex(),
		Transactions: anotherBlockTransactions,
	}

	// Запись блока 15960495 в Firebase для примера
	err = writeToFirebase(firebaseURL, anotherBlockData)
	if err != nil {
		log.Fatalf("Ошибка записи в Firebase: %v", err)
	}
	fmt.Println("Данные блока 15960495 записаны в Firebase.")

	// В реальном мониторинге мы можем периодически проверять новые блоки и обновлять данные в Firebase
	// Ниже для примера сделаем небольшой цикл, который каждые 30 секунд проверяет новый последний блок
	// и при появлении нового блока записывает его данные в Firebase.
	fmt.Println("Начинаем мониторинг новых блоков...")
	lastKnownBlock := block.Number().Uint64()
	for {
		time.Sleep(30 * time.Second)
		latestHeader, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			log.Println("Ошибка получения последнего блока:", err)
			continue
		}
		latestBlockNum := latestHeader.Number.Uint64()
		if latestBlockNum > lastKnownBlock {
			// Получаем новый блок
			newBlock, err := client.BlockByNumber(context.Background(), latestHeader.Number)
			if err != nil {
				log.Println("Ошибка получения нового блока:", err)
				continue
			}

			// Получаем транзакции нового блока
			var newTransactions []Transaction
			for _, tx := range newBlock.Transactions() {
				toAddr := ""
				if tx.To() != nil {
					toAddr = tx.To().Hex()
				}
				t := Transaction{
					ChainId:  tx.ChainId().String(),
					Hash:     tx.Hash().Hex(),
					Value:    tx.Value().String(),
					Cost:     tx.Cost().String(),
					To:       toAddr,
					Gas:      tx.Gas(),
					GasPrice: tx.GasPrice().String(),
				}
				newTransactions = append(newTransactions, t)
			}

			newBlockData := BlockData{
				Number:       newBlock.Number().Uint64(),
				Time:         newBlock.Time(),
				Difficulty:   newBlock.Difficulty().Uint64(),
				Hash:         newBlock.Hash().Hex(),
				Transactions: newTransactions,
			}

			err = writeToFirebase(firebaseURL, newBlockData)
			if err != nil {
				log.Println("Ошибка записи нового блока в Firebase:", err)
				continue
			}
			fmt.Printf("Новый блок %d записан в Firebase.\n", newBlock.Number().Uint64())

			lastKnownBlock = newBlock.Number().Uint64()
		}
	}
}

// writeToFirebase записывает данные о блоке в Firebase по пути blocks/<block_number>.json
func writeToFirebase(firebaseURL string, blockData BlockData) error {
	url := fmt.Sprintf("%s/blocks/%d.json", firebaseURL, blockData.Number)
	jsonData, err := json.Marshal(blockData)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ошибка записи в Firebase, статус: %s", resp.Status)
	}
	return nil
}
