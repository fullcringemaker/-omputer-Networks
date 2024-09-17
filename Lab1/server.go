package main

import (
        "encoding/json"
        "fmt"
        "github.com/mgutz/logxi/v1"
        "io"
        "net"
        "strings"
        "strconv"
        "sync"
        "lab1/src/proto"
)

var mutex sync.Mutex 

const (
        Reset       = "\033[0m"
        IPColor     = "\033[1;32m" 
)

type Client struct {
        logger log.Logger    
        conn   *net.TCPConn  
        enc    *json.Encoder 
}

func NewClient(conn *net.TCPConn) *Client {
        return &Client{
                logger: log.New(fmt.Sprintf("client %s", conn.RemoteAddr().String())),
                conn:   conn,
                enc:    json.NewEncoder(conn),
        }
}

func (client *Client) serve() {
        defer client.conn.Close()
        decoder := json.NewDecoder(client.conn)
        for {
                var req proto.Request
                if err := decoder.Decode(&req); err != nil {
                        if err == io.EOF {
                                client.logger.Info("client disconnected", "address", client.conn.RemoteAddr().String())
                                break
                        }
                        client.logger.Error("cannot decode message", "reason", err)
                        break
                }
                client.logger.Info("received request", "number", req.Number, "digit", req.Digit)
                client.handleRequest(req)
        }
}

func (client *Client) handleRequest(req proto.Request) {
        if len(req.Digit) != 1 || !strings.Contains("0123456789", req.Digit) {
                client.respond("error", "Invalid digit", 0)
                return
        }
        if _, err := strconv.Atoi(req.Number); err != nil {
                client.respond("error", "Invalid number", 0)
                return
        }
        count := strings.Count(req.Number, req.Digit)
        client.respond("ok", "", count)
}

func (client *Client) respond(status string, message string, count int) {
        response := proto.Response{
                Status:  status,
                Message: message,
                Count:   count,
        }
        if err := client.enc.Encode(response); err != nil {
                client.logger.Error("cannot send response", "reason", err)
        }
}

func main() {
        addrStr := "185.102.139.169:9742"
        logger := log.New("server")

        addr, err := net.ResolveTCPAddr("tcp", addrStr)
        if err != nil {
                logger.Error("address resolution failed", "address", addrStr, "reason", err)
                return
        }
        listener, err := net.ListenTCP("tcp", addr)
        if err != nil {
                logger.Error("listening failed", "reason", err)
                return
        }
        defer listener.Close()
        fmt.Printf("The server started on %s%s%s\n", IPColor, addr.String(), Reset)
        logger.Info("server started", "address", addr.String())
        for {
                conn, err := listener.AcceptTCP()
                if err != nil {
                        logger.Error("cannot accept connection", "reason", err)
                        continue
                }
                logger.Info("accepted connection", "address", conn.RemoteAddr().String())
                go NewClient(conn).serve()
        }
}
