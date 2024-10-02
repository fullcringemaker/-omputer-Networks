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

type Message struct {
	ID         string   `json:"id"`
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`
	Content    string   `json:"content"`
	HopCount   int      `json:"hop_count"`
	MaxHops    int      `json:"max_hops"`
	Timestamp  int64    `json:"timestamp"`
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

	// WebSocket related variables
	clients      = make(map[*websocket.Conn]bool)
	clientsMutex sync.Mutex
	upgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// List of all peers
	allPeers = []Peer{
		{Name: "Peer1", Address: "185.104.251.226:9765"},
		{Name: "Peer2", Address: "185.102.139.161:9765"},
		{Name: "Peer3", Address: "185.102.139.168:9765"},
		{Name: "Peer4", Address: "185.102.139.169:9765"},
	}
)

type Peer struct {
	Name    string `json:"name"`
	Address string `json:"address"`
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

	// Start WebSocket server
	go startWebServer()

	// Start listening for P2P connections
	go startListening()

	// Connect to next peer
	go connectToNextPeer()

	// Handle user commands
	handleCommands()
}

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

	messageMutex.Lock()
	defer messageMutex.Unlock()

	for _, m := range receivedMessages {
		if m.ID == msg.ID {
			logEvent("Already received message %s. Discarding.", msg.ID)
			return
		}
	}

	// Check if the message is intended for this peer
	isForUs := false
	for _, recipient := range msg.Recipients {
		if recipient == peerName {
			isForUs = true
			break
		}
	}

	if isForUs {
		receivedMessages = append(receivedMessages, *msg)
		logEvent("Message %s is for us. Handling.", msg.ID)

		// Broadcast to WebSocket clients
		broadcastMessage(msg)

		consoleMutex.Lock()
		fmt.Printf("\nReceived message from %s: %s\n", msg.Sender, msg.Content)
		fmt.Print("Enter command: ")
		consoleMutex.Unlock()

		// Remove this peer from recipients
		newRecipients := []string{}
		for _, recipient := range msg.Recipients {
			if recipient != peerName {
				newRecipients = append(newRecipients, recipient)
			}
		}
		msg.Recipients = newRecipients
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
		go connectToNextPeer()
		return
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
			sendMessage(recipients, content)
		case "print":
			printReceivedMessages()
		default:
			fmt.Println("Unknown command. Available commands: send, print")
		}
	}
}

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

func startWebServer() {
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleWebSocket)
	addr := "0.0.0.0:9651"
	logEvent("Starting web server on %s", addr)
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		logError("Web server failed: %v", err)
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	// Serve the index.html file
	http.ServeFile(w, r, "index.html")
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logError("WebSocket upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	// Register client
	clientsMutex.Lock()
	clients[ws] = true
	clientsMutex.Unlock()

	// Send current state to the new client
	messageMutex.Lock()
	initialMessages := make([]Message, len(receivedMessages))
	copy(initialMessages, receivedMessages)
	messageMutex.Unlock()

	for _, msg := range initialMessages {
		err := ws.WriteJSON(msg)
		if err != nil {
			logError("WebSocket write error: %v", err)
			return
		}
	}

	// Keep the connection open
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()
			break
		}
	}
}

func broadcastMessage(msg *Message) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()
	for client := range clients {
		err := client.WriteJSON(msg)
		if err != nil {
			logError("WebSocket broadcast error: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
}
