package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Структура сообщения
type Message struct {
	ID         string   `json:"id"`
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`
	Content    string   `json:"content"`
	HopCount   int      `json:"hop_count"`
	MaxHops    int      `json:"max_hops"`
	Timestamp  int64    `json:"timestamp"`
}

// Структура для передачи сообщений в WebSocket
type WSMessage struct {
	Recipient string `json:"recipient"`
	Sender    string `json:"sender"`
	Content   string `json:"content"`
}

// Глобальные переменные для P2P
var (
	peerName         string
	ownAddress       string
	ownPort          string
	nextPeerAddr     string
	nextPeerPort     string
	nextPeerConn     net.Conn
	listener         net.Listener
	receivedMessages []Message
	messageMutex     sync.Mutex
	logMutex         sync.Mutex
	consoleMutex     sync.Mutex
)

// Глобальные переменные для WebSocket
var (
	upgrader      = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	wsClients     = make(map[*websocket.Conn]bool)
	clientsMutex  sync.Mutex
	broadcastChan = make(chan WSMessage)
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Name: ")
	peerNameInput, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading Name: %v\n", err)
		os.Exit(1)
	}
	peerName = strings.TrimSpace(peerNameInput)

	fmt.Print("Enter your IP address and port (ip:port): ")
	ownAddrInput, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading own IP and port: %v\n", err)
		os.Exit(1)
	}
	ownAddr := strings.TrimSpace(ownAddrInput)

	fmt.Print("Enter next peer IP address and port (ip:port): ")
	nextPeerAddrPortInput, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading next peer IP and port: %v\n", err)
		os.Exit(1)
	}
	nextPeerAddrPort := strings.TrimSpace(nextPeerAddrPortInput)

	ownAddressPort := strings.Split(ownAddr, ":")
	if len(ownAddressPort) != 2 {
		fmt.Println("Invalid own IP address and port format. Expected format ip:port")
		os.Exit(1)
	}
	ownAddress = ownAddressPort[0]
	ownPort = ownAddressPort[1]

	nextPeerAddressPort := strings.Split(nextPeerAddrPort, ":")
	if len(nextPeerAddressPort) != 2 {
		fmt.Println("Invalid next peer IP address and port format. Expected format ip:port")
		os.Exit(1)
	}
	nextPeerAddr = nextPeerAddressPort[0]
	nextPeerPort = nextPeerAddressPort[1]

	initLogging()

	// Запуск HTTP-сервера
	go startHTTPServer()

	// Запуск P2P-слушателя
	go startListening()

	// Подключение к следующему пиру
	go connectToNextPeer()

	// Запуск обработчика трансляции сообщений в WebSocket
	go handleBroadcast()

	// Обработка команд из консоли
	handleCommands()
}

// Инициализация логирования
func initLogging() {
	logFile, err := os.OpenFile(fmt.Sprintf("%s.log", peerName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("Failed to open log file: %v\n", err)
		os.Exit(1)
	}
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	logEvent("Peer %s started. Listening on %s:%s", peerName, ownAddress, ownPort)
}

// Запуск HTTP-сервера
func startHTTPServer() {
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/send", handleSendMessage) // Регистрация нового обработчика

	addr := ownAddress + ":9651"
	logEvent("Starting HTTP server on %s", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		logError("Failed to start HTTP server: %v", err)
		os.Exit(1)
	}
}

// Обработчик главной страницы
func serveHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

// Обработчик WebSocket соединений
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logError("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Добавление клиента в список
	clientsMutex.Lock()
	wsClients[conn] = true
	clientsMutex.Unlock()

	logEvent("New WebSocket client connected: %s", conn.RemoteAddr().String())

	// Ожидание закрытия соединения
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logError("WebSocket read error: %v", err)
			}
			break
		}
	}

	// Удаление клиента из списка при закрытии
	clientsMutex.Lock()
	delete(wsClients, conn)
	clientsMutex.Unlock()

	logEvent("WebSocket client disconnected: %s", conn.RemoteAddr().String())
}

// Новый обработчик для отправки сообщений через HTTP GET-запросы
// Пример запроса: /send?from=Peer1&to=Peer2&msg=Hello
func handleSendMessage(w http.ResponseWriter, r *http.Request) {
	// Разрешить только GET-запросы
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed. Use GET.", http.StatusMethodNotAllowed)
		return
	}

	// Парсинг параметров
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	msg := r.URL.Query().Get("msg")

	// Валидация параметров
	if from == "" || to == "" || msg == "" {
		http.Error(w, "Missing parameters. Required: from, to, msg", http.StatusBadRequest)
		return
	}

	// Проверка, что отправитель совпадает с текущим пиром
	if from != peerName {
		http.Error(w, "Invalid sender. Sender must match the current peer's name.", http.StatusForbidden)
		return
	}

	// Создание сообщения
	recipients := strings.Split(to, ",")
	content := msg

	message := Message{
		ID:         generateMessageID(),
		Sender:     from,
		Recipients: recipients,
		Content:    content,
		HopCount:   0,
		MaxHops:    10,
		Timestamp:  time.Now().Unix(),
	}

	logEvent("HTTP request to send message %s from %s to %v", message.ID, message.Sender, message.Recipients)

	// Отправка сообщения
	forwardMessage(&message)

	// Возврат успешного ответа
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Message %s sent successfully.", message.ID)))
}

// Обработка P2P соединений
func startListening() {
	addr := ownAddress + ":" + ownPort
	var err error
	listener, err = net.Listen("tcp", addr)
	if err != nil {
		logError("Failed to start listening: %v", err)
		os.Exit(1)
	}
	logEvent("Listening for incoming connections on %s", addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			logError("Failed to accept connection: %v", err)
			continue
		}
		logEvent("Accepted connection from %s", conn.RemoteAddr().String())
		go handleConnection(conn)
	}
}

// Обработка входящего соединения
func handleConnection(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				logError("Error reading from connection: %v", err)
			}
			break
		}
		var msg Message
		err = json.Unmarshal(line, &msg)
		if err != nil {
			logError("Failed to unmarshal message: %v", err)
			continue
		}
		err = validateMessage(&msg)
		if err != nil {
			logError("Invalid message received: %v", err)
			continue
		}
		receiveMessage(&msg)
	}
}

// Проверка валидности сообщения
func validateMessage(msg *Message) error {
	if msg.ID == "" {
		return errors.New("message ID is empty")
	}
	if msg.Sender == "" {
		return errors.New("sender is empty")
	}
	if len(msg.Recipients) == 0 {
		return errors.New("recipients list is empty")
	}
	if msg.Content == "" {
		return errors.New("content is empty")
	}
	if msg.HopCount < 0 {
		return errors.New("invalid hop count")
	}
	if msg.MaxHops <= 0 {
		return errors.New("invalid max hops")
	}
	return nil
}

// Обработка полученного сообщения
func receiveMessage(msg *Message) {
	logEvent("Received message %s from %s", msg.ID, msg.Sender)

	if msg.HopCount >= msg.MaxHops {
		logEvent("Message %s reached max hops. Discarding.", msg.ID)
		return
	}

	messageMutex.Lock()
	defer messageMutex.Unlock()

	for _, m := range receivedMessages {
		if m.ID == msg.ID {
			logEvent("Already received message %s. Discarding.", msg.ID)
			return
		}
	}

	receivedMessages = append(receivedMessages, *msg)

	for i, recipient := range msg.Recipients {
		if recipient == peerName {
			logEvent("Message %s is for us. Handling.", msg.ID)

			consoleMutex.Lock()
			fmt.Printf("\nReceived message from %s: %s\n", msg.Sender, msg.Content)
			fmt.Print("Enter command: ")
			consoleMutex.Unlock()

			// Создание WSMessage и отправка в канал broadcast
			wsMsg := WSMessage{
				Recipient: peerName,
				Sender:    msg.Sender,
				Content:   msg.Content,
			}
			broadcastChan <- wsMsg

			// Удаление текущего получателя из списка
			msg.Recipients = append(msg.Recipients[:i], msg.Recipients[i+1:]...)
			break
		}
	}

	msg.HopCount++

	if len(msg.Recipients) > 0 {
		forwardMessage(msg)
	} else {
		logEvent("All recipients handled for message %s. Not forwarding.", msg.ID)
	}
}

// Отправка сообщения через P2P
func forwardMessage(msg *Message) {
	if nextPeerConn == nil {
		go connectToNextPeer()
		// Подождать некоторое время для установления соединения
		time.Sleep(1 * time.Second)
	}
	if nextPeerConn != nil {
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			logError("Failed to marshal message %s: %v", msg.ID, err)
			return
		}
		msgBytes = append(msgBytes, '\n')
		_, err = nextPeerConn.Write(msgBytes)
		if err != nil {
			logError("Failed to forward message %s: %v", msg.ID, err)
			nextPeerConn.Close()
			nextPeerConn = nil
			return
		}
		logEvent("Forwarded message %s to %s:%s", msg.ID, nextPeerAddr, nextPeerPort)
	} else {
		logError("No connection to next peer. Cannot forward message %s", msg.ID)
	}
}

// Подключение к следующему пиру
func connectToNextPeer() {
	for {
		addr := nextPeerAddr + ":" + nextPeerPort
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			logError("Failed to connect to next peer %s:%s: %v. Retrying in 5 seconds.", nextPeerAddr, nextPeerPort, err)
			time.Sleep(5 * time.Second)
			continue
		}
		nextPeerConn = conn
		logEvent("Connected to next peer %s:%s", nextPeerAddr, nextPeerPort)
		return
	}
}

// Обработка команд из консоли
func handleCommands() {
	reader := bufio.NewReader(os.Stdin)
	for {
		consoleMutex.Lock()
		fmt.Print("Enter command: ")
		consoleMutex.Unlock()

		cmdLine, err := reader.ReadString('\n')
		if err != nil {
			logError("Failed to read command: %v", err)
			continue
		}
		cmdLine = strings.TrimSpace(cmdLine)
		if cmdLine == "" {
			continue
		}
		parts := strings.Fields(cmdLine)
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "send":
			if len(parts) < 3 {
				fmt.Println("Usage: send <recipient1,recipient2,...> <message>")
				continue
			}
			recipients := strings.Split(parts[1], ",")
			content := strings.Join(parts[2:], " ")
			sendMessage(recipients, content)
		case "print":
			printReceivedMessages()
		default:
			fmt.Println("Unknown command. Available commands: send, print")
		}
	}
}

// Отправка сообщения
func sendMessage(recipients []string, content string) {
	msg := Message{
		ID:         generateMessageID(),
		Sender:     peerName,
		Recipients: recipients,
		Content:    content,
		HopCount:   0,
		MaxHops:    10,
		Timestamp:  time.Now().Unix(),
	}
	logEvent("Sending message %s to %v", msg.ID, recipients)
	forwardMessage(&msg)
}

// Генерация уникального ID сообщения
func generateMessageID() string {
	return fmt.Sprintf("%s-%d", peerName, time.Now().UnixNano())
}

// Печать полученных сообщений
func printReceivedMessages() {
	messageMutex.Lock()
	defer messageMutex.Unlock()
	if len(receivedMessages) == 0 {
		fmt.Println("No messages received.")
		return
	}
	fmt.Println("Received messages:")
	for _, msg := range receivedMessages {
		fmt.Printf("From: %s; Content: %s\n", msg.Sender, msg.Content)
	}
}

// Логирование событий
func logEvent(format string, v ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	log.Printf("EVENT: "+format, v...)
}

// Логирование ошибок
func logError(format string, v ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	log.Printf("ERROR: "+format, v...)
}

// Обработка трансляции сообщений в WebSocket
func handleBroadcast() {
	for {
		msg := <-broadcastChan
		messageBytes, err := json.Marshal(msg)
		if err != nil {
			logError("Failed to marshal WSMessage: %v", err)
			continue
		}
		messageStr := string(messageBytes)

		clientsMutex.Lock()
		for client := range wsClients {
			err := client.WriteMessage(websocket.TextMessage, []byte(messageStr))
			if err != nil {
				logError("WebSocket write error: %v", err)
				client.Close()
				delete(wsClients, client)
			}
		}
		clientsMutex.Unlock()
	}
}
