package main

import (
	"bufio"
	"fmt"
	"net"
	"time"
)

func handleConnection(conn net.Conn) {
	defer conn.Close()

	for {
		message := time.Now().Format("15:04:05") + " Работает :)\n"

		_, err := conn.Write([]byte(message))
		if err != nil {
			fmt.Println("Ошибка при отправлении данных:", err)
			return
		}

		time.Sleep(time.Second)
	}
}

func main() {
	conn, err := net.Dial("tcp", "localhost:9742")
	if err != nil {
		fmt.Println("Ошибка при подключении к серверу:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Подключено к серверу")

	reader := bufio.NewReader(conn)

	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Ошибка при чтении данных:", err)
			return
		}

		fmt.Print(message)
	}
}
