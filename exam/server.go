package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
)

// Настройка WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true 
	},
}
func homeHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("index.html"))
	tmpl.Execute(w, nil)
}
func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка при обновлении соединения:", err)
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("Ошибка при чтении сообщения:", err)
			break
		}
		num, err := strconv.Atoi(string(msg))
		if err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte(""))
			continue
		}
		result := num * num
		response := fmt.Sprintf("%d", result)

		err = conn.WriteMessage(websocket.TextMessage, []byte(response))
		if err != nil {
			log.Println("Ошибка при отправке сообщения:", err)
			break
		}
	}
}

func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/ws", wsHandler)

	log.Println("Сервер запущен на http://185.102.139.168:9742")
	err := http.ListenAndServe("185.102.139.168:9742", nil)
	if err != nil {
		log.Fatal("Ошибка запуска сервера:", err)
	}
}
