// protoc.go
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Message структура сообщения
type Message struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"` // 'message' или 'notification'
	Sender         string   `json:"sender"` // Для 'message': отправитель; Для 'notification': пир, отправивший уведомление
	SenderIP       string   `json:"sender_ip"` // IP отправителя или пира, отправившего уведомление
	SenderPort     string   `json:"sender_port"` // Порт отправителя или пира, отправившего уведомление
	Recipients     []string `json:"recipients"` // Для 'message': список получателей; Для 'notification': пусто
	Content        string   `json:"content"` // Для 'message': содержание сообщения; Для 'notification': содержание исходного сообщения
	OriginalSender string   `json:"original_sender,omitempty"` // Для 'notification': оригинальный отправитель
	OriginalIP     string   `json:"original_ip,omitempty"`     // Для 'notification': IP оригинального отправителя
	OriginalPort   string   `json:"original_port,omitempty"`   // Для 'notification': порт оригинального отправителя
	HopCount       int      `json:"hop_count"`
	MaxHops        int      `json:"max_hops"`
	Timestamp      int64    `json:"timestamp"`
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

	// Для уведомлений и хранения информации о пирах
	processedNotifications map[string]bool
	allReceivedMessages    map[string][]Message // map[recipientPeerName][]Message
	peerInfo               map[string]string    // map[peerName]ip:port

	// WebSocket переменные
	upgrader      = websocket.Upgrader{}
	wsClients     = make(map[*websocket.Conn]bool)
	wsClientsLock sync.Mutex
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

	processedNotifications = make(map[string]bool)
	allReceivedMessages = make(map[string][]Message)
	peerInfo = make(map[string]string)

	go startListening()
	go connectToNextPeer()
	go startHTTPServer()

	handleCommands()
}

// initLogging инициализирует логирование в файл
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

// startListening запускает TCP-сервер для приёма P2P-сообщений
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

// handleConnection обрабатывает входящее соединение
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

// validateMessage проверяет корректность сообщения
func validateMessage(msg *Message) error {
	if msg.ID == "" {
		return errors.New("message ID is empty")
	}
	if msg.Sender == "" {
		return errors.New("sender is empty")
	}
	if msg.Type == "message" {
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
	} else if msg.Type == "notification" {
		if msg.Content == "" {
			return errors.New("content is empty")
		}
		if msg.SenderIP == "" || msg.SenderPort == "" {
			return errors.New("sender IP or port is empty in notification")
		}
		if msg.OriginalSender == "" {
			return errors.New("original sender is empty in notification")
		}
		if msg.OriginalIP == "" || msg.OriginalPort == "" {
			return errors.New("original sender IP or port is empty in notification")
		}
	}
	return nil
}

// receiveMessage обрабатывает полученное сообщение
func receiveMessage(msg *Message) {
	logEvent("Received %s message %s from %s", msg.Type, msg.ID, msg.Sender)

	if msg.HopCount >= msg.MaxHops {
		logEvent("Message %s reached max hops. Discarding.", msg.ID)
		return
	}

	messageMutex.Lock()
	defer messageMutex.Unlock()

	if msg.Type == "message" {
		// Обработка обычного сообщения
		isRecipient := false
		for _, recipient := range msg.Recipients {
			if recipient == peerName {
				isRecipient = true
				break
			}
		}

		if isRecipient {
			// Добавляем сообщение в список полученных
			receivedMessages = append(receivedMessages, *msg)
			if _, exists := allReceivedMessages[peerName]; !exists {
				allReceivedMessages[peerName] = []Message{}
			}
			allReceivedMessages[peerName] = append(allReceivedMessages[peerName], *msg)
			// Отправляем обновленный список на веб-интерфейс
			broadcastAllReceivedMessages()

			consoleMutex.Lock()
			fmt.Printf("\nReceived message from %s: %s\n", msg.Sender, msg.Content)
			consoleMutex.Unlock()

			// Если сообщение адресовано самому себе, не пересылаем уведомление
			if msg.Sender == peerName {
				return
			}

			// Создаем уведомление
			notification := Message{
				ID:             msg.ID + "-notif",
				Type:           "notification",
				Sender:         peerName, // пир, который получает сообщение
				SenderIP:       ownAddress,
				SenderPort:     ownPort,
				Content:        msg.Content,
				OriginalSender: msg.Sender,
				OriginalIP:     msg.SenderIP,
				OriginalPort:   msg.SenderPort,
				HopCount:       0,
				MaxHops:        10,
				Timestamp:      msg.Timestamp,
			}

			forwardMessage(&notification)
		} else {
			// Сообщение не для этого пира, просто пересылаем
			forwardMessage(msg)
		}
	} else if msg.Type == "notification" {
		// Обработка уведомления
		if processedNotifications[msg.ID] {
			// Уже обработано, игнорируем
			logEvent("Already processed notification %s. Discarding.", msg.ID)
			return
		}

		// Помечаем уведомление как обработанное
		processedNotifications[msg.ID] = true

		// Сохраняем информацию о получателе сообщения
		peerInfo[msg.Sender] = fmt.Sprintf("%s:%s", msg.SenderIP, msg.SenderPort)

		// Добавляем сообщение в общий список полученных сообщений
		if _, exists := allReceivedMessages[msg.Sender]; !exists {
			allReceivedMessages[msg.Sender] = []Message{}
		}
		allReceivedMessages[msg.Sender] = append(allReceivedMessages[msg.Sender], Message{
			Sender:         msg.OriginalSender,
			Content:        msg.Content,
			Timestamp:      msg.Timestamp,
			SenderIP:       "", // Не требуется для отображения
			SenderPort:     "",
			OriginalSender: "",
			OriginalIP:     "",
			OriginalPort:   "",
		})

		// Отправляем обновленный список на веб-интерфейс
		broadcastAllReceivedMessages()

		// Пересылаем уведомление дальше по кольцу
		forwardMessage(msg)
	}
}

// forwardMessage пересылает сообщение следующему пирy
func forwardMessage(msg *Message) {
	if msg.Type == "notification" && msg.OriginalSender == peerName {
		// Не пересылаем уведомления обратно отправителю
		return
	}

	// Если сообщение адресовано самому себе, не пересылаем
	if msg.Type == "message" {
		for _, recipient := range msg.Recipients {
			if recipient == peerName {
				return
			}
		}
	}

	if nextPeerConn == nil {
		go connectToNextPeer()
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
		logEvent("Forwarded %s message %s to %s:%s", msg.Type, msg.ID, nextPeerAddr, nextPeerPort)
	} else {
		logError("No connection to next peer. Cannot forward message %s", msg.ID)
	}
}

// connectToNextPeer устанавливает соединение с следующим пирy
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

// handleCommands обрабатывает команды пользователя из консоли
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

// sendMessage создаёт и пересылает сообщение
func sendMessage(recipients []string, content string) {
	msg := Message{
		ID:             generateMessageID(),
		Type:           "message",
		Sender:         peerName,
		SenderIP:       ownAddress,
		SenderPort:     ownPort,
		Recipients:     recipients,
		Content:        content,
		HopCount:       0,
		MaxHops:        10,
		Timestamp:      time.Now().Unix(),
	}
	logEvent("Sending message %s to %v", msg.ID, recipients)
	forwardMessage(&msg)
}

// generateMessageID генерирует уникальный ID для сообщения
func generateMessageID() string {
	return fmt.Sprintf("%s-%d", peerName, time.Now().UnixNano())
}

// printReceivedMessages выводит полученные сообщения
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

// logEvent записывает событие в лог
func logEvent(format string, v ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	log.Printf("EVENT: "+format, v...)
}

// logError записывает ошибку в лог
func logError(format string, v ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	log.Printf("ERROR: "+format, v...)
}

// startHTTPServer запускает HTTP-сервер для веб-интерфейса и WebSocket
func startHTTPServer() {
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleWebSocket)

	addr := ownAddress + ":9651"
	logEvent("Starting HTTP server on %s", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		logError("HTTP server failed: %v", err)
		os.Exit(1)
	}
}

// serveHome обслуживает HTML-страницу
func serveHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

// handleWebSocket устанавливает соединение WebSocket
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool { return true } // Разрешить все источники
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logError("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	wsClientsLock.Lock()
	wsClients[conn] = true
	wsClientsLock.Unlock()
	logEvent("WebSocket client connected: %s", conn.RemoteAddr().String())

	for {
		// Чтение сообщений от клиента не требуется, поэтому просто поддерживаем соединение
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	wsClientsLock.Lock()
	delete(wsClients, conn)
	wsClientsLock.Unlock()
	logEvent("WebSocket client disconnected: %s", conn.RemoteAddr().String())
}

// broadcastAllReceivedMessages отправляет весь список полученных сообщений всем WebSocket клиентам
func broadcastAllReceivedMessages() {
	wsClientsLock.Lock()
	defer wsClientsLock.Unlock()

	// Создаем структуру данных для отправки на веб-интерфейс
	displayData := make(map[string][]map[string]string)
	for peer, messages := range allReceivedMessages {
		displayData[peer] = []map[string]string{}
		for _, msg := range messages {
			displayData[peer] = append(displayData[peer], map[string]string{
				"sender":  msg.Sender,
				"content": msg.Content,
			})
		}
	}

	data := map[string]interface{}{
		"allReceivedMessages": displayData,
	}

	msgBytes, err := json.Marshal(data)
	if err != nil {
		logError("Failed to marshal WebSocket message: %v", err)
		return
	}

	for client := range wsClients {
		err := client.WriteMessage(websocket.TextMessage, msgBytes)
		if err != nil {
			logError("WebSocket write error: %v", err)
			client.Close()
			delete(wsClients, client)
		}
	}
}
