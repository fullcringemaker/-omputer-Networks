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
)

const (
	infuraURL   = "https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191"
	firebaseURL = "https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app"
)

// Структуры для JSON-RPC запросов и ответов от инфуры
type jsonRPCRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	Id      int           `json:"id"`
}

type jsonRPCResponseBlock struct {
	Jsonrpc string          `json:"jsonrpc"`
	Id      int             `json:"id"`
	Result  *RPCBlockResult `json:"result"`
}

type RPCBlockResult struct {
	Number       string          `json:"number"`
	Hash         string          `json:"hash"`
	Timestamp    string          `json:"timestamp"`
	Difficulty   string          `json:"difficulty"`
	Transactions []RPCTransaction `json:"transactions"`
}

type RPCTransaction struct {
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	From             string `json:"from"`
	Gas              string `json:"gas"`
	GasPrice         string `json:"gasPrice"`
	Hash             string `json:"hash"`
	Input            string `json:"input"`
	Nonce            string `json:"nonce"`
	To               string `json:"to"`
	TransactionIndex string `json:"transactionIndex"`
	Value            string `json:"value"`
	V                string `json:"v"`
	R                string `json:"r"`
	S                string `json:"s"`
}

type jsonRPCResponseLatestBlockNumber struct {
	Jsonrpc string `json:"jsonrpc"`
	Id      int    `json:"id"`
	Result  string `json:"result"`
}

// Структуры для записи данных в Firebase
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
}

// Выполняется JSON-RPC запрос к инфуре
func callInfura(method string, params []interface{}, result interface{}) error {
	// Формирование запроса
	reqData := jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		Id:      1,
	}

	reqBytes, err := json.Marshal(reqData)
	if err != nil {
		return err
	}

	// Отправка POST-запроса к инфуре
	resp, err := http.Post(infuraURL, "application/json", bytes.NewBuffer(reqBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Декодирование ответа в result
	return json.NewDecoder(resp.Body).Decode(result)
}

// Получение номера последнего блока из инфуры, номер приходит в hex-формате и конвертируется
func getLatestBlockNumber() (int64, error) {
	var response jsonRPCResponseLatestBlockNumber
	err := callInfura("eth_blockNumber", []interface{}{}, &response)
	if err != nil {
		return 0, err
	}

	// Конвертация номера блока из hex в целочисленное значение
	num, ok := new(big.Int).SetString(response.Result[2:], 16)
	if !ok {
		return 0, fmt.Errorf("unable to parse block number")
	}
	return num.Int64(), nil
}

// Получение данных о блоке по номеру, с использованием метода eth_getBlockByNumber
func getBlockByNumber(blockNum int64) (*RPCBlockResult, error) {
	hexNum := fmt.Sprintf("0x%x", blockNum) // Формирование hex-представления номера блока
	var response jsonRPCResponseBlock
	err := callInfura("eth_getBlockByNumber", []interface{}{hexNum, true}, &response)
	if err != nil {
		return nil, err
	}
	if response.Result == nil {
		return nil, fmt.Errorf("no block result for %d", blockNum)
	}
	return response.Result, nil
}

// Запись данных о блоке в Firebase, используется метод PUT для обновления конкретного узла
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

	// Отправка HTTP-запроса в Firebase
	clientHttp := &http.Client{}
	resp, err := clientHttp.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Проверка кода ответа на успешность
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("failed to write block data to firebase: status code %d", resp.StatusCode)
	}

	return nil
}

// Запись транзакций в Firebase, аналогично записи блока
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

	// Проверка успешности записи
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("failed to write transactions data to firebase: status code %d", resp.StatusCode)
	}

	return nil
}

// Конвертация hex-значения в uint64
func hexToUint64(hexStr string) (uint64, error) {
	num, ok := new(big.Int).SetString(hexStr[2:], 16)
	if !ok {
		return 0, fmt.Errorf("unable to parse hex: %s", hexStr)
	}
	return num.Uint64(), nil
}

func main() {
	ctx := context.Background()
	_ = ctx // Контекст пока не задействован активно

	// Получение текущего последнего номера блока при старте
	latestBlock, err := getLatestBlockNumber()
	if err != nil {
		log.Fatalln("Error getting latest block number:", err)
	}
	currentBlock := latestBlock

	// Запуск цикла мониторинга, периодически проверяются новые блоки
	for {
		newLatestBlock, err := getLatestBlockNumber()
		if err != nil {
			log.Println("Error getting latest block number:", err)
			time.Sleep(15 * time.Second)
			continue
		}

		// Если появился новый блок, обрабатывается каждый новый блок по порядку
		if newLatestBlock > currentBlock {
			for bNum := currentBlock + 1; bNum <= newLatestBlock; bNum++ {
				// Получение данных о блоке
				blockDataRPC, err := getBlockByNumber(bNum)
				if err != nil {
					log.Println("Error fetching block:", err)
					continue
				}

				// Парсинг ключевых значений блока
				blockNumber, err := hexToUint64(blockDataRPC.Number)
				if err != nil {
					log.Println("Error parsing block number:", err)
					continue
				}

				blockTime, err := hexToUint64(blockDataRPC.Timestamp)
				if err != nil {
					log.Println("Error parsing block time:", err)
					continue
				}

				blockDiff, err := hexToUint64(blockDataRPC.Difficulty)
				if err != nil {
					log.Println("Error parsing block difficulty:", err)
					continue
				}

				// Подготовка данных блока для записи в Firebase
				bData := BlockData{
					Number:     blockNumber,
					Time:       blockTime,
					Difficulty: blockDiff,
					Hash:       blockDataRPC.Hash,
					TxCount:    len(blockDataRPC.Transactions),
				}

				// Запись данных блока в Firebase
				if err := writeBlockToFirebase(bData); err != nil {
					log.Println("Error writing block data to Firebase:", err)
				} else {
					fmt.Println("Block data written to Firebase for block:", bData.Number)
				}

				// Формирование списка транзакций для записи в Firebase
				var txs []TransactionData
				for _, tx := range blockDataRPC.Transactions {
					gas, err := hexToUint64(tx.Gas)
					if err != nil {
						gas = 0
					}
					txData := TransactionData{
						Hash:     tx.Hash,
						Value:    tx.Value,
						To:       tx.To,
						Gas:      gas,
						GasPrice: tx.GasPrice,
					}
					txs = append(txs, txData)
				}

				// Запись транзакций блока в Firebase
				if err := writeTransactionsToFirebase(blockNumber, txs); err != nil {
					log.Println("Error writing transactions to Firebase:", err)
				} else {
					fmt.Println("Transactions data written to Firebase for block:", blockNumber)
				}
			}
			// Обновление текущего обработанного номера блока
			currentBlock = newLatestBlock
		}

		// Пауза перед следующей проверкой на новые блоки
		time.Sleep(15 * time.Second)
	}
}
