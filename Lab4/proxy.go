package main

import (
    "bytes"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "net/url"
    "path"
    "strings"
    "time"

    "github.com/patrickmn/go-cache"
    "golang.org/x/net/html"
)

// Кэш с временем жизни 10 минут и периодом очистки 15 минут
var c = cache.New(10*time.Minute, 15*time.Minute)

func main() {
    // Настройка HTTP-сервера
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
    parts := strings.SplitN(r.URL.Path[1:], "/", 2)
    if len(parts) == 0 {
        http.Error(w, "Invalid request format. Use /<domain>/<path>", http.StatusBadRequest)
        return
    }

    domain := parts[0]
    targetPath := ""
    if len(parts) > 1 {
        targetPath = parts[1]
    }

    // Формируем целевый URL
    targetURL := fmt.Sprintf("https://%s/%s", domain, targetPath)
    fmt.Printf("Fetching URL: %s\n", targetURL)

    // Проверяем кэш
    if cachedResponse, found := c.Get(targetURL); found {
        fmt.Println("Serving from cache.")
        w.Header().Set("Content-Type", "text/html")
        w.Write(cachedResponse.([]byte))
        return
    }

    // Выполняем запрос к целевому серверу
    resp, err := http.Get(targetURL)
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

    // Сохраняем в кэш
    c.Set(targetURL, modifiedBody, cache.DefaultExpiration)

    // Возвращаем модифицированный контент клиенту
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
            if n.Data == "a" {
                attrName = "href"
            } else if n.Data == "link" {
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
                            // Извлекаем домен из оригинального URL
                            targetDomain := parsedURL.Host
                            if targetDomain == "" {
                                // Относительный URL, используем текущий домен
                                host := r.Host
                                hostParts := strings.Split(host, ":")
                                targetDomain = hostParts[0]
                            }

                            // Переписываем URL через прокси
                            newURL := fmt.Sprintf("https://%s:9742/%s%s", getServerHost(), targetDomain, parsedURL.Path)
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

// Функция для получения хоста прокси-сервера
func getServerHost() string {
    // Предполагается, что прокси-сервер доступен через net1.yss.su, net2.yss.su и т.д.
    // Здесь можно реализовать балансировку нагрузки или выбор конкретного сервера
    return "net1.yss.su"
}

