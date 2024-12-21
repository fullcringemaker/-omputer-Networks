package main

import (
	"bufio"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strings"
)

const (
	pop3ServerAddress = "mail.nic.ru:110"
	pop3User          = "2@dactyl.su"
	pop3Pass          = "12345678990DactylSUDTS"
)

var dec = new(mime.WordDecoder)

func decodeSubject(s string) string {
	decoded, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}
func getMailSubjects() []string {
	conn, _ := net.Dial("tcp", pop3ServerAddress)
	defer conn.Close()
	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	sendCommand(conn, reader, "USER "+pop3User)
	sendCommand(conn, reader, "PASS "+pop3Pass)
	count := getMailCount(conn, reader)
	subjects := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		writeLine(conn, fmt.Sprintf("RETR %d", i))
		resp, _ := reader.ReadString('\n')
		if !strings.HasPrefix(resp, "+OK") {
			continue
		}
		var subject string
		for {
			line, _ := reader.ReadString('\n')
			line = strings.TrimRight(line, "\r\n")
			if line == "." {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "subject:") {
				raw := strings.TrimSpace(line[len("subject:"):])
				subject = decodeSubject(raw)
			}
		}
		if subject == "" {
			subject = "(no topic)"
		}
		subjects = append(subjects, subject)
	}
	sendCommand(conn, reader, "QUIT")
	return subjects
}
func deleteAllMails() {
	conn, _ := net.Dial("tcp", pop3ServerAddress)
	defer conn.Close()
	reader := bufio.NewReader(conn)
	reader.ReadString('\n')
	sendCommand(conn, reader, "USER "+pop3User)
	sendCommand(conn, reader, "PASS "+pop3Pass)
	count := getMailCount(conn, reader)
	for i := 1; i <= count; i++ {
		sendCommand(conn, reader, fmt.Sprintf("DELE %d", i))
	}
	sendCommand(conn, reader, "QUIT")
}
func getMailCount(conn net.Conn, reader *bufio.Reader) int {
	writeLine(conn, "STAT")
	line, _ := reader.ReadString('\n')
	parts := strings.Split(line, " ")
	var count int
	fmt.Sscanf(parts[1], "%d", &count)
	return count
}
func sendCommand(conn net.Conn, reader *bufio.Reader, cmd string) {
	writeLine(conn, cmd)
	reader.ReadString('\n')
}
func writeLine(conn net.Conn, cmd string) {
	conn.Write([]byte(cmd + "\r\n"))
}
func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}
func listHandler(w http.ResponseWriter, r *http.Request) {
	subs := getMailSubjects()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, "[")
	for i, s := range subs {
		esc := strings.ReplaceAll(s, `"`, `\"`)
		fmt.Fprintf(w, "\"%s\"", esc)
		if i < len(subs)-1 {
			fmt.Fprint(w, ",")
		}
	}
	fmt.Fprintf(w, "]")
}
func deleteAllHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST", http.StatusMethodNotAllowed)
		return
	}
	deleteAllMails()
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
func main() {
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/api/messages", listHandler)
	http.HandleFunc("/api/deleteAll", deleteAllHandler)
	http.ListenAndServe(":9742", nil)
}
