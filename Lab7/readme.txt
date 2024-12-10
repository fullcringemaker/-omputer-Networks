Реализовать приложение мониторинга состояния заданных блоков блокчейн
Etherium. Данные результатов мониторинга должны записываться в Firebase.
Аккаунт в infura.io необходимо зарегистрировать свой, также создать собственную
Realtime Database.
Транзакции должны записывать в Firebase и обновляться, после обновления данных в блоках

6dd88c2f98b241eb8e15033618275191 - это ключ от infura
https://mainnet.infura.io/v3/6dd88c2f98b241eb8e15033618275191 - ссылка, которую мне дали на infura

https://etherium-realtime-transactions-default-rtdb.europe-west1.firebasedatabase.app/ - ссылка, которую мне дали для таблицы firebase

Напиши мне программу на языке GO, все вышеперчисленное и ниже перечисленное должно быть в 1 файле 1 кодом и точно выполнять все то, что указано в коде. 


Исходные коды (данные могут отличаться от тех, что я дал ранее. нужно заменить на мои):

Получение последнего блока

package main
import (
 "context"
 "fmt"
 "github.com/ethereum/go-ethereum/ethclient"
 "log"
 "math/big"
)
func main() {
 client, err := ethclient.Dial("https://mainnet.infura.io/v3/8133ff0c11dc491daac3f680d2f74d18")
 if err != nil {
 log.Fatalln(err)
 }
 header, err := client.HeaderByNumber(context.Background(), nil)
 if err != nil {
 log.Fatal(err)
 }
 fmt.Println(header.Number.String()) // The lastes block in blockchain because nil pointer in header
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

Получение данных из блока по номеру

package main
import (
 "context"
 "fmt"
 "github.com/ethereum/go-ethereum/ethclient"
 "log"
 "math/big"
)
func main() {
 client, err := ethclient.Dial("https://mainnet.infura.io/v3/8133ff0c11dc491daac3f680d2f74d18")
 if err != nil {
 log.Fatalln(err)
 }
 blockNumber := big.NewInt(15960495)
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

Получение данных из полей транзакции

package main
import (
 "context"
 "fmt"
 "github.com/ethereum/go-ethereum/ethclient"
 "log"
 "math/big"
)
func main() {
 client, err := ethclient.Dial("https://mainnet.infura.io/v3/8133ff0c11dc491daac3f680d2f74d18")
 if err != nil {
 log.Fatalln(err)
 }
 blockNumber := big.NewInt(15960495)
 block, err := client.BlockByNumber(context.Background(), blockNumber) //get block with this number
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


