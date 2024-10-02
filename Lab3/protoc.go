package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket" // Убедитесь, что пакет установлен
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

// Структура пиринга
type Peer struct {
	Name    string
	Address string // Формат ip:port
}

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

	// Список всех пиров в сети
	allPeers = []Peer{
		{Name: "Peer1", Address: "185.104.251.226:9765"},
		{Name: "Peer2", Address: "185.102.139.161:9765"},
		{Name: "Peer3", Address: "185.102.139.168:9765"},
		{Name: "Peer4", Address: "185.102.139.169:9765"},
	}

	// WebSocket
	upgrader      = websocket.Upgrader{}
	clients       = make(map[*websocket.Conn]bool)
	clientsMutex  sync.Mutex
	broadcastChan = make(chan WSMessage)
)

// Структура сообщения для WebSocket
type WSMessage struct {
	Destination string `json:"destination"`
	Content     string `json:"content"`
	Sender      string `json:"sender"`
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	// Ввод имени пира
	fmt.Print("Name: ")
	peerNameInput, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading Name: %v\n", err)
		os.Exit(1)
	}
	peerName = strings.TrimSpace(peerNameInput)

	// Ввод собственного IP и порта
	fmt.Print("Enter your IP address and port (ip:port): ")
	ownAddrInput, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading own IP and port: %v\n", err)
		os.Exit(1)
	}
	ownAddr := strings.TrimSpace(ownAddrInput)

	// Ввод следующего пира
	fmt.Print("Enter next peer IP address and port (ip:port): ")
	nextPeerAddrPortInput, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading next peer IP and port: %v\n", err)
		os.Exit(1)
	}
	nextPeerAddrPort := strings.TrimSpace(nextPeerAddrPortInput)

	// Разделение собственного адреса и порта
	ownAddressPort := strings.Split(ownAddr, ":")
	if len(ownAddressPort) != 2 {
		fmt.Println("Invalid own IP address and port format. Expected format ip:port")
		os.Exit(1)
	}
	ownAddress = ownAddressPort[0]
	ownPort = ownAddressPort[1]

	// Разделение адреса и порта следующего пира
	nextPeerAddressPort := strings.Split(nextPeerAddrPort, ":")
	if len(nextPeerAddressPort) != 2 {
		fmt.Println("Invalid next peer IP address and port format. Expected format ip:port")
		os.Exit(1)
	}
	nextPeerAddr = nextPeerAddressPort[0]
	nextPeerPort = nextPeerAddressPort[1]

	// Инициализация логирования
	initLogging()

	// Запуск HTTP сервера для веб-интерфейса
	go startHTTPServer()

	// Запуск прослушивания входящих соединений
	go startListening()

	// Подключение к следующему пирру
	go connectToNextPeer()

	// Обработка команд из консоли
	handleCommands()

	// Обработка сообщений для WebSocket
	go handleBroadcast()
}

// Инициализация логирования в файл
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

// Запуск HTTP сервера
func startHTTPServer() {
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleWebSocket)

	addr := "0.0.0.0:9651"
	logEvent("Starting HTTP server on %s", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		logError("HTTP server failed: %v", err)
		os.Exit(1)
	}
}

// Обработчик главной страницы
func serveHome(w http.ResponseWriter, r *http.Request) {
	// Разрешить все источники для упрощения
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	if r.URL.Path != "/" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, "index.html")
}

// Обработчик WebSocket соединений
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Разрешить все источники для упрощения
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logError("WebSocket upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	clientsMutex.Lock()
	clients[ws] = true
	clientsMutex.Unlock()

	logEvent("WebSocket client connected: %s", ws.RemoteAddr().String())

	// Блокируем до закрытия соединения
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()
			logEvent("WebSocket client disconnected: %s", ws.RemoteAddr().String())
			break
		}
	}
}

// Обработка сообщений для отправки в WebSocket
func handleBroadcast() {
	for {
		msg := <-broadcastChan
		clientsMutex.Lock()
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				logError("WebSocket write error: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
		clientsMutex.Unlock()
	}
}

// Запуск прослушивания входящих TCP соединений
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

// Обработка входящего TCP соединения
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

// Валидация сообщения
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

// Обработка принятого сообщения
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

	// Проверка, предназначено ли сообщение этому пиру
	for i, recipient := range msg.Recipients {
		if recipient == peerName {
			logEvent("Message %s is for us. Handling.", msg.ID)

			consoleMutex.Lock()
			fmt.Printf("\nReceived message from %s: %s\n", msg.Sender, msg.Content)
			fmt.Print("Enter command: ")
			consoleMutex.Unlock()

			// Удаление получателя из списка
			msg.Recipients = append(msg.Recipients[:i], msg.Recipients[i+1:]...)
			break
		}
	}

	// Отправка сообщения в WebSocket
	for _, recipient := range msg.Recipients {
		if peerInfo, ok := getPeerByName(recipient); ok {
			wsMsg := WSMessage{
				Destination: fmt.Sprintf("%s (%s)", peerInfo.Name, peerInfo.Address),
				Content:     msg.Content,
				Sender:      msg.Sender,
			}
			broadcastChan <- wsMsg
		}
	}

	msg.HopCount++

	if len(msg.Recipients) > 0 {
		forwardMessage(msg)
	} else {
		logEvent("All recipients handled for message %s. Not forwarding.", msg.ID)
	}
}

// Отправка сообщения следующему пирру
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

// Подключение к следующему пирру
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

// Генерация уникального ID для сообщения
func generateMessageID() string {
	return fmt.Sprintf("%s-%d", peerName, time.Now().UnixNano())
}

// Вывод полученных сообщений
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

// Получение информации о пире по имени
func getPeerByName(name string) (Peer, bool) {
	for _, peer := range allPeers {
		if peer.Name == name {
			return peer, true
		}
	}
	return Peer{}, false
}
