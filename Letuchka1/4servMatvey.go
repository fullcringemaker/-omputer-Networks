package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var hashPattern = regexp.MustCompile(`[a-f0-9]{32}`)

func getPassword(hash string) (string, error) {
	apiURL := fmt.Sprintf("http://pstgu.yss.su/iu9/networks/let1/getkey.php?hash=%s", hash)
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("ошибка при запросе пароля: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	return "", fmt.Errorf("пароль не найден в ответе")
}

func sendRequest(password string) {
	fio := "Трофименко Дмитрий Иванович"
	subject := "let1_ИУ9-32Б_Посевин_Данила"

	escapedURL := fmt.Sprintf(
		"http://pstgu.yss.su/iu9/networks/let1_2024/send_from_go.php?subject=%s&fio=%s&pass=%s",
		url.QueryEscape(subject),
		url.QueryEscape(fio),
		url.QueryEscape(strings.TrimPrefix(password, "pass: ")),
	)

	fmt.Printf("Отправляем запрос: %s\n", escapedURL)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(escapedURL)
	if err != nil {
		log.Fatalf("ошибка при отправке запроса: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("ошибка при чтении тела ответа: %v", err)
	}

	fmt.Printf("Статус ответа: %s\n", resp.Status)
	fmt.Printf("Тело ответа: %s\n", string(body))
}

func main() {
	cmd := exec.Command("sh", "-c", "tcpdump -v -l -A | grep key:")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("ошибка при создании канала вывода: %v", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("ошибка при запуске команды: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	fmt.Println("Начинаем слушать tcpdump...")
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println("Получено:", line)

		if strings.Contains(line, "Trofimenko") {
			match := hashPattern.FindString(line)
			if match != "" {
				fmt.Printf("Найден хэш: %s\n", match)

				password, err := getPassword(match)
				if err != nil {
					log.Fatalf("ошибка получения пароля: %v", err)
				}
				fmt.Printf("Получен пароль: %s\n", password)

				sendRequest(password)
				break
			} else {
				fmt.Println("Хэш не найден в строке.")
			}
		} else {
			fmt.Println("Строка не содержит Trofimenko")
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("ошибка при чтении данных: %v", err)
	}
}
