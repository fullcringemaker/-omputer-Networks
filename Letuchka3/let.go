package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
)

const (
	pop3Addr = "mail.nic.ru:110"        // POP3-сервер и порт
	pop3User = "2@dactyl.su"            // Имя пользователя
	pop3Pass = "12345678990DactylSUDTS" // Пароль
)

func main() {
	http.Handle("/", http.FileServer(http.Dir(".")))
	http.HandleFunc("/delete-emails", deleteEmailsHandler)

	log.Println("Сервер запущен на порту :9742")
	log.Fatal(http.ListenAndServe(":9742", nil))
}

func deleteEmailsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conn, err := net.Dial("tcp", pop3Addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Не удалось подключиться к POP3-серверу: %v", err), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	if err := checkOK(reader); err != nil {
		http.Error(w, fmt.Sprintf("Сервер вернул ошибку при подключении: %v", err), http.StatusInternalServerError)
		return
	}
	if err := sendCmdCheckOK(conn, reader, "USER "+pop3User+"\r\n"); err != nil {
		http.Error(w, fmt.Sprintf("Ошибка при отправке USER: %v", err), http.StatusInternalServerError)
		return
	}
	if err := sendCmdCheckOK(conn, reader, "PASS "+pop3Pass+"\r\n"); err != nil {
		http.Error(w, fmt.Sprintf("Ошибка при отправке PASS: %v", err), http.StatusInternalServerError)
		return
	}
	if err := sendCmd(conn, "STAT\r\n"); err != nil {
		http.Error(w, fmt.Sprintf("Ошибка при отправке STAT: %v", err), http.StatusInternalServerError)
		return
	}

	statResp, err := readLine(reader)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка при чтении ответа STAT: %v", err), http.StatusInternalServerError)
		return
	}
	count, size, err := parseStat(statResp)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка при парсинге STAT: %v", err), http.StatusInternalServerError)
		return
	}
	for i := 1; i <= count; i++ {
		cmd := fmt.Sprintf("DELE %d\r\n", i)
		if err := sendCmdCheckOK(conn, reader, cmd); err != nil {
			http.Error(w, fmt.Sprintf("Ошибка при удалении письма №%d: %v", i, err), http.StatusInternalServerError)
			return
		}
	}
	if err := sendCmdCheckOK(conn, reader, "QUIT\r\n"); err != nil {
		http.Error(w, fmt.Sprintf("Ошибка при отправке QUIT: %v", err), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(fmt.Sprintf("Успешно удалено %d писем (общий размер: %d байт).", count, size)))
}
func sendCmd(conn net.Conn, cmd string) error {
	_, err := conn.Write([]byte(cmd))
	return err
}
func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
func checkOK(reader *bufio.Reader) error {
	line, err := readLine(reader)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "+OK") {
		return fmt.Errorf("сервер вернул ошибку: %s", line)
	}
	return nil
}
func sendCmdCheckOK(conn net.Conn, reader *bufio.Reader, cmd string) error {
	if err := sendCmd(conn, cmd); err != nil {
		return err
	}
	return checkOK(reader)
}
func parseStat(line string) (count, size int, err error) {
	parts := strings.Split(line, " ")
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("неверный формат ответа STAT: %s", line)
	}
	if parts[0] != "+OK" {
		return 0, 0, fmt.Errorf("сервер вернул ошибку: %s", line)
	}
	count, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("не удалось преобразовать количество писем: %v", err)
	}
	size, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, fmt.Errorf("не удалось преобразовать размер почты: %v", err)
	}
	return count, size, nil
}
