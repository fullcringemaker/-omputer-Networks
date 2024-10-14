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

// Маппинг хэшей к паролям
var hashToPassword = map[string]string{
	"98b9a579005de69e994e58d0f1156177": "pass: fdgf4gtt", // Новый хэш и соответствующий пароль
	// Можно добавить другие хэши и пароли по необходимости
}

// Функция для получения пароля по хэшу
func getPassword(hash string) (string, error) {
	// Проверяем, есть ли хэш в заранее заданном маппинге
	if pass, exists := hashToPassword[hash]; exists {
		fmt.Printf("Используем заранее заданный пароль для хэша %s: %s\n", hash, pass)
		return pass, nil
	}

	// Если хэш не найден в маппинге, пытаемся получить пароль с сервера
	urlStr := fmt.Sprintf("http://pstgu.yss.su/iu9/networks/let1/getkey.php?hash=%s", hash)
	fmt.Printf("Отправляем запрос на получение пароля: %s\n", urlStr)

	resp, err := http.Get(urlStr)
	if err != nil {
		return "", fmt.Errorf("Ошибка при запросе пароля: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем статус ответа
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Неожиданный статус ответа: %s", resp.Status)
	}

	// Читаем весь ответ для отладки
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Ошибка при чтении ответа: %w", err)
	}
	response := strings.TrimSpace(string(body))
	fmt.Printf("Ответ сервера: %s\n", response)

	// Проверяем наличие префикса "pass: "
	prefix := "pass: "
	if strings.HasPrefix(response, prefix) {
		return response, nil
	}

	// Если префикс отсутствует, возвращаем ошибку с полным ответом для анализа
	return "", fmt.Errorf("Пароль не найден в ответе. Ответ сервера: %s", response)
}

// Функция для отправки запроса с найденным паролем и ФИО
func sendRequest(password string) {
	fio := "Трофименко Дмитрий Иванович"
	subject := "let1_ИУ9-32Б_Посевин_Данила"

	// Удаляем префикс "pass: " из пароля, если он присутствует
	pass := strings.TrimPrefix(password, "pass: ")

	// Проверяем, что пароль не пустой после удаления префикса
	if pass == password {
		log.Printf("Предупреждение: Пароль не содержит ожидаемый префикс 'pass: '")
	}

	// Формируем URL с параметрами запроса
	urlStr := fmt.Sprintf(
		"http://pstgu.yss.su/iu9/networks/let1_2024/send_from_go.php?subject=%s&fio=%s&pass=%s",
		url.QueryEscape(subject),
		url.QueryEscape(fio),
		url.QueryEscape(pass),
	)

	fmt.Printf("Отправляем запрос: %s\n", urlStr)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(urlStr)
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
	// Запускаем tcpdump с фильтром по "key:"
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
