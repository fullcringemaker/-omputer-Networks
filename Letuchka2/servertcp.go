package main

import (
	"fmt"
	"net"
	"time"
)

func handleConnection(conn net.Conn) {
	defer conn.Close()

	for {
		message := time.Now().Format("15:04:05") + " Hello\n"

		_, err := conn.Write([]byte(message))
		if err != nil {
			fmt.Println("Ошибка при отправлении данных:", err)
			return
		}

		time.Sleep(time.Second)
	}
}

func main() {
	listener, err := net.Listen("tcp", ":9742")
	if err != nil {
		fmt.Println("Ошибка при запуске сервера:", err)
		return
	}
	defer listener.Close()

	fmt.Println("Сервер запущен на порту 9742")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Ошибка при принятии подключения:", err)
			continue
		}

		go handleConnection(conn)
	}
}
