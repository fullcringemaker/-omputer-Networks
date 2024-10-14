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

// Регулярное выражение для поиска MD5-хэша
var hashPattern = regexp.MustCompile(`[a-f0-9]{32}`)

// Функция для получения пароля по хэшу
func getPassword(hash string) (string, error) {
	url := fmt.Sprintf("http://pstgu.yss.su/iu9/networks/let1/getkey.php?hash=%s", hash)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("Ошибка при запросе пароля: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	return "", fmt.Errorf("Пароль не найден в ответе")
}

// Функция для отправки запроса с найденным паролем и ФИО
func sendRequest(password string) {
	fio := "Трофименко Дмитрий Иванович"
	url := fmt.Sprintf(
		"http://pstgu.yss.su/iu9/networks/let1_2024/send_from_go.php?subject=%s&fio=%s&pass=%s",
		url.QueryEscape("let1_ИУ9-32Б_Посевин_Данила"),
		url.QueryEscape(fio),
		url.QueryEscape(strings.TrimPrefix(password, "pass: ")),
	)

	fmt.Printf("Отправляем запрос: %s\n", url)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Fatalf("Ошибка при отправке запроса: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Ошибка при чтении тела ответа: %v", err)
	}

	fmt.Printf("Статус ответа: %s\n", resp.Status)
	fmt.Printf("Тело ответа: %s\n", string(body))
}

func main() {
	// Запускаем tcpdump с grep для фильтрации строк по "key:"
	cmd := exec.Command("sh", "-c", "tcpdump -v -l -A | grep key:")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Ошибка при создании канала вывода: %v", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("Ошибка при запуске команды: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println("Получено:", line) // Отладочный вывод

		// Проверяем, содержит ли строка "Trofimenko"
		if strings.Contains(line, "Trofimenko") {
			// Ищем MD5-хэш в строке
			match := hashPattern.FindString(line)
			if match != "" {
				fmt.Printf("Найден хэш: %s\n", match)

				// Получаем пароль по хэшу
				password, err := getPassword(match)
				if err != nil {
					log.Fatalf("Ошибка получения пароля: %v", err)
				}
				fmt.Printf("Получен пароль: %s\n", password)

				// Отправляем запрос с паролем и ФИО
				sendRequest(password)
				break // Завершаем после успешной отправки
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Ошибка при чтении данных: %v", err)
	}
}
