package main

import (
    "bytes"
    "fmt"
    "io"
    "log"
    "net/http"
    "net/url"
    "strings"
    "sync"
    "time"

    "github.com/patrickmn/go-cache"
    "golang.org/x/net/html"
)

// Инициализация кэша с временем жизни 10 минут и периодом очистки 15 минут
var c = cache.New(10*time.Minute, 15*time.Minute)

// Мьютекс для безопасного доступа к серверному хосту при многопоточности
var mu sync.Mutex
var serverIndex int
var servers = []string{
    "185.104.251.226:9742",
    "185.102.139.161:9742",
    "185.102.139.168:9742",
    "185.102.139.169:9742",
}

// Структура для хранения кэшированного ответа
type CachedResponse struct {
    Headers map[string]string
    Body    []byte
}

func main() {
    // Настройка обработчика прокси-запросов
    http.HandleFunc("/", proxyHandler)

    serverAddress := ":9742"
    fmt.Printf("Starting proxy server on %s...\n", serverAddress)
    if err := http.ListenAndServe(serverAddress, nil); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
}

// Обработчик прокси-запросов
func proxyHandler(w http.ResponseWriter, r *http.Request) {
    // Ожидаем URL вида /<domain>/<path>
    parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
    if len(parts) == 0 || parts[0] == "" {
        http.Error(w, "Invalid request format. Use /<domain>/<path>", http.StatusBadRequest)
        return
    }

    domain := parts[0]
    targetPath := ""
    if len(parts) > 1 {
        targetPath = parts[1]
    }

    // Формируем целевой URL
    targetURL := fmt.Sprintf("https://%s/%s", domain, targetPath)
    fmt.Printf("Fetching URL: %s\n", targetURL)

    // Проверяем кэш
    if cachedResponse, found := c.Get(targetURL); found {
        fmt.Println("Serving from cache.")
        for k, v := range cachedResponse.(CachedResponse).Headers {
            w.Header().Set(k, v)
        }
        w.Write(cachedResponse.(CachedResponse).Body)
        return
    }

    // Выполняем запрос к целевому серверу
    client := &http.Client{
        Timeout: 30 * time.Second,
    }

    req, err := http.NewRequest("GET", targetURL, nil)
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
        return
    }

    // Выполняем запрос
    resp, err := client.Do(req)
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to fetch target URL: %v", err), http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    // Читаем тело ответа
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to read response body: %v", err), http.StatusInternalServerError)
        return
    }

    // Парсим и модифицируем HTML
    modifiedBody, err := rewriteHTML(body, r)
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to parse HTML: %v", err), http.StatusInternalServerError)
        return
    }

    // Копируем заголовки из оригинального ответа
    headers := make(map[string]string)
    for k, v := range resp.Header {
        if len(v) > 0 {
            headers[k] = v[0]
        }
    }

    // Сохраняем в кэш
    c.Set(targetURL, CachedResponse{
        Headers: headers,
        Body:    modifiedBody,
    }, cache.DefaultExpiration)

    // Возвращаем модифицированный контент клиенту
    for k, v := range headers {
        // Избегаем перезаписи некоторых заголовков
        if k != "Content-Length" && k != "Transfer-Encoding" {
            w.Header().Set(k, v)
        }
    }
    w.Header().Set("Content-Type", "text/html")
    w.Write(modifiedBody)
}

// Функция переписывания HTML-контента
func rewriteHTML(body []byte, r *http.Request) ([]byte, error) {
    doc, err := html.Parse(bytes.NewReader(body))
    if err != nil {
        return nil, err
    }

    var f func(*html.Node)
    f = func(n *html.Node) {
        if n.Type == html.ElementNode {
            var attrName string
            if n.Data == "a" || n.Data == "link" {
                attrName = "href"
            } else if n.Data == "script" || n.Data == "img" {
                attrName = "src"
            }

            if attrName != "" {
                for i, attr := range n.Attr {
                    if attr.Key == attrName {
                        originalURL := attr.Val
                        // Обрабатываем только относительные и абсолютные URL
                        parsedURL, err := url.Parse(originalURL)
                        if err == nil && (parsedURL.Scheme == "http" || parsedURL.Scheme == "https" || parsedURL.IsAbs()) {
                            var targetDomain string
                            if parsedURL.Host != "" {
                                targetDomain = parsedURL.Host
                            } else {
                                // Относительный URL, используем текущий домен
                                host := r.Host
                                hostParts := strings.Split(host, ":")
                                targetDomain = hostParts[0]
                            }

                            // Переписываем URL через прокси
                            newURL := fmt.Sprintf("http://185.104.251.226:9742/%s%s", targetDomain, parsedURL.Path)
                            if parsedURL.RawQuery != "" {
                                newURL += "?" + parsedURL.RawQuery
                            }
                            if parsedURL.Fragment != "" {
                                newURL += "#" + parsedURL.Fragment
                            }

                            n.Attr[i].Val = newURL
                        }
                    }
                }
            }
        }

        // Рекурсивный вызов для дочерних узлов
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            f(c)
        }
    }

    f(doc)

    // Рендерим изменённый HTML обратно в []byte
    var buf bytes.Buffer
    if err := html.Render(&buf, doc); err != nil {
        return nil, err
    }

    return buf.Bytes(), nil
}
