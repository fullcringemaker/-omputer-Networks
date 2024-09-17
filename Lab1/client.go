package main

import (
        "encoding/json"
        "flag"
        "fmt"
        "github.com/skorobogatov/input"
        "net"
        "lab1/src/proto"
)

const (
        Reset       = "\033[0m"
        NumberColor = "\033[1;34m" 
        DigitColor  = "\033[1;33m" 
        CountColor  = "\033[1;32m" 
        ErrorColor  = "\033[1;31m" 
)

func interact(conn *net.TCPConn) {
        defer conn.Close()
        encoder, decoder := json.NewEncoder(conn), json.NewDecoder(conn)

        for {
                fmt.Printf("%sEnter number: %s", NumberColor, Reset)
                number := input.Gets()
                fmt.Printf("%sEnter digit: %s", DigitColor, Reset)
                digit := input.Gets()
                request := proto.Request{
                        Number: number,
                        Digit:  digit,
                }
                if err := encoder.Encode(&request); err != nil {
                        fmt.Printf("%sError: cannot send request: %v%s\n", ErrorColor, err, Reset)
                        return
                }
                var response proto.Response
                if err := decoder.Decode(&response); err != nil {
                        fmt.Printf("%sError: cannot decode response: %v%s\n", ErrorColor, err, Reset)
                        return
                }
                switch response.Status {
                case "ok":
                        fmt.Printf("Digit %s%s%s occurs %s%s%s times in the number %s%s%s\n",
                                DigitColor, digit, Reset,
                                CountColor, fmt.Sprintf("%d", response.Count), Reset,
                                NumberColor, number, Reset)
                case "error":
                        fmt.Printf("%sError: %s%s\n", ErrorColor, response.Message, Reset)
                }
        }
}

func main() {
        var addrStr string
        flag.StringVar(&addrStr, "addr", "185.102.139.169:9742", "specify IP address and port")
        flag.Parse()

        if addr, err := net.ResolveTCPAddr("tcp", addrStr); err != nil {
                fmt.Printf("%sError: %v%s\n", ErrorColor, err, Reset)
        } else if conn, err := net.DialTCP("tcp", nil, addr); err != nil {
                fmt.Printf("%sError: %v%s\n", ErrorColor, err, Reset)
        } else {
                interact(conn)
        }
}

