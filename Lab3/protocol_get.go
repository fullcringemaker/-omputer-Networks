// protoc.go
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
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

// Структура для обработки POST-запроса на отправку сообщения
type SendMessageRequest struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Message   string `json:"message"`
}

// Переменные для P2P
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

// Переменные для WebSocket
var (
	upgrader     = websocket.Upgrader{}
	clients      = make(map[*websocket.Conn]bool)
	clientsMutex sync.Mutex
	peerList     = []PeerInfo{
		{Name: "Peer1", IP: "185.104.251.226", Port: "9651"},
		{Name: "Peer2", IP: "185.102.139.161", Port: "9651"},
		{Name: "Peer3", IP: "185.102.139.168", Port: "9651"}, // Исправлено
		{Name: "Peer4", IP: "185.102.139.169", Port: "9651"}, // Исправлено
	}
}

// Структура информации о пирах для HTML
type PeerInfo struct {
	Name string
	IP   string
	Port string
}

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

	// Запуск P2P слушателя
	go startListening()

	// Подключение к следующему пиру
	go connectToNextPeer()

	// Запуск HTTP сервера
	go startHTTPServer()

	// Обработка команд
	handleCommands()
}

func initLogging() {
	logFile, err := os.OpenFile(fmt.Sprintf("%s.log", peerName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("Failed to open log file: %v\n", err)
		os.Exit(1)
	}
	log.SetOutput(logFile) // Логи только в файл
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	logEvent("Peer %s started. Listening on %s:%s", peerName, ownAddress, ownPort)
}

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

func receiveMessage(msg *Message) {
	logEvent("Received message %s from %s", msg.ID, msg.Sender)

	if msg.HopCount >= msg.MaxHops {
		logEvent("Message %s reached max hops. Discarding.", msg.ID)
		return
	}

	// Проверяем, адресовано ли сообщение этому пиру
	isRecipient := false
	for i, recipient := range msg.Recipients {
		if recipient == peerName {
			isRecipient = true

			// Удаляем этот пир из списка получателей
			msg.Recipients = append(msg.Recipients[:i], msg.Recipients[i+1:]...)
			break
		}
	}

	if isRecipient {
		messageMutex.Lock()
		// Проверяем, было ли уже получено это сообщение
		for _, m := range receivedMessages {
			if m.ID == msg.ID {
				logEvent("Already received message %s. Discarding.", msg.ID)
				messageMutex.Unlock()
				return
			}
		}

		// Добавляем сообщение в список полученных
		receivedMessages = append(receivedMessages, *msg)
		messageMutex.Unlock()

		logEvent("Message %s is for us. Handling.", msg.ID)

		consoleMutex.Lock()
		fmt.Printf("\nReceived message from %s: %s\n", msg.Sender, msg.Content)
		fmt.Print("Enter command: ")
		consoleMutex.Unlock()

		// Отправка сообщения через WebSocket
		broadcastMessage(fmt.Sprintf("Received message from %s: %s", msg.Sender, msg.Content))
	}

	msg.HopCount++

	if len(msg.Recipients) > 0 {
		forwardMessage(msg)
	} else {
		logEvent("All recipients handled for message %s. Not forwarding.", msg.ID)
	}
}

func forwardMessage(msg *Message) {
	if nextPeerConn == nil {
		connectToNextPeer()
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
			sendMessage(peerName, recipients, content)
		case "print":
			printReceivedMessages()
		default:
			fmt.Println("Unknown command. Available commands: send, print")
		}
	}
}

// sendMessageFrom позволяет отправлять сообщение от указанного отправителя
func sendMessageFrom(sender string, recipients []string, content string) {
	msg := Message{
		ID:         generateMessageID(),
		Sender:     sender,
		Recipients: recipients,
		Content:    content,
		HopCount:   0,
		MaxHops:    10,
		Timestamp:  time.Now().Unix(),
	}
	logEvent("Sending message %s from %s to %v", msg.ID, sender, recipients)
	forwardMessage(&msg)
}

// sendMessage отправляет сообщение от указанного отправителя
func sendMessage(sender string, recipients []string, content string) {
	sendMessageFrom(sender, recipients, content)
}

func generateMessageID() string {
	return fmt.Sprintf("%s-%d", peerName, time.Now().UnixNano())
}

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

// Функции логирования
func logEvent(format string, v ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	log.Printf("EVENT: "+format, v...)
}

func logError(format string, v ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	log.Printf("ERROR: "+format, v...)
}

// Функции для WebSocket

func startHTTPServer() {
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/send", handleSendMessage) // Новый обработчик
	logEvent("Starting HTTP server on port 9651")
	err := http.ListenAndServe(":9651", nil)
	if err != nil {
		logError("HTTP server error: %v", err)
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("index.html")
	if err != nil {
		logError("Failed to parse index.html: %v", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	err = tmpl.Execute(w, nil)
	if err != nil {
		logError("Failed to execute template: %v", err)
		http.Error(w, "Internal Server Error", 500)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool { return true } // Разрешить все источники
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logError("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	clientsMutex.Lock()
	clients[conn] = true
	clientsMutex.Unlock()

	logEvent("WebSocket client connected: %s", conn.RemoteAddr().String())

	// Чтение сообщений от клиента (если необходимо)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			clientsMutex.Lock()
			delete(clients, conn)
			clientsMutex.Unlock()
			logEvent("WebSocket client disconnected: %s", conn.RemoteAddr().String())
			break
		}
	}
}

// Обработчик POST и GET запросов на отправку сообщений
func handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		handleSendMessagePost(w, r)
	} else if r.Method == http.MethodGet {
		handleSendMessageGet(w, r)
	} else {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// Обработчик POST-запросов на отправку сообщений
func handleSendMessagePost(w http.ResponseWriter, r *http.Request) {
	var req SendMessageRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		logError("Failed to decode send message request: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Валидация полей
	if req.Sender == "" || req.Recipient == "" || req.Message == "" {
		http.Error(w, "Missing sender, recipient or message", http.StatusBadRequest)
		return
	}

	// Проверка, существует ли отправитель и получатель
	validSender := false
	validRecipient := false
	for _, peer := range peerList {
		if peer.Name == req.Sender {
			validSender = true
		}
		if peer.Name == req.Recipient {
			validRecipient = true
		}
	}
	if !validSender {
		http.Error(w, "Invalid sender name", http.StatusBadRequest)
		return
	}
	if !validRecipient {
		http.Error(w, "Invalid recipient name", http.StatusBadRequest)
		return
	}

	// Отправка сообщения
	sendMessageFrom(req.Sender, []string{req.Recipient}, req.Message)

	// Ответ клиенту
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{"status": "success"}
	json.NewEncoder(w).Encode(resp)
}

// Обработчик GET-запросов на отправку сообщений
func handleSendMessageGet(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	sender := query.Get("from")
	recipient := query.Get("to")
	message := query.Get("msg")

	// Валидация полей
	if sender == "" || recipient == "" || message == "" {
		http.Error(w, "Missing 'from', 'to' or 'msg' query parameters", http.StatusBadRequest)
		return
	}

	// Проверка, существует ли отправитель и получатель
	validSender := false
	validRecipient := false
	for _, peer := range peerList {
		if peer.Name == sender {
			validSender = true
		}
		if peer.Name == recipient {
			validRecipient = true
		}
	}
	if !validSender {
		http.Error(w, "Invalid sender name", http.StatusBadRequest)
		return
	}
	if !validRecipient {
		http.Error(w, "Invalid recipient name", http.StatusBadRequest)
		return
	}

	// Отправка сообщения
	sendMessageFrom(sender, []string{recipient}, message)

	// Ответ клиенту
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{"status": "success"}
	json.NewEncoder(w).Encode(resp)
}

// Функция для отправки сообщений через P2P сеть от указанного отправителя
func sendMessageFrom(sender string, recipients []string, content string) {
	msg := Message{
		ID:         generateMessageID(),
		Sender:     sender,
		Recipients: recipients,
		Content:    content,
		HopCount:   0,
		MaxHops:    10,
		Timestamp:  time.Now().Unix(),
	}
	logEvent("Sending message %s from %s to %v", msg.ID, sender, recipients)
	forwardMessage(&msg)
}

// Функция для отправки сообщений всем подключенным WebSocket клиентам
func broadcastMessage(message string) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	for client := range clients {
		err := client.WriteMessage(websocket.TextMessage, []byte(message))
		if err != nil {
			logError("WebSocket send error: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
}
