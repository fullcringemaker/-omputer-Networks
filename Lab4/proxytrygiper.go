package main

import (
    "bytes"
    "fmt"
    "io"
    "log"
    "net/http"
    "net/url"
    "regexp"
    "strings"
    "sync"

    "golang.org/x/net/html"
)

const (
    proxyPort = 9742
    proxyIP   = "185.104.251.226" // Замените на IP вашего сервера
)

// Cache структура для кэширования
type Cache struct {
    data map[string][]byte
    mu   sync.RWMutex
}

func NewCache() *Cache {
    return &Cache{
        data: make(map[string][]byte),
    }
}

func (c *Cache) Get(key string) ([]byte, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    val, exists := c.data[key]
    return val, exists
}

func (c *Cache) Set(key string, value []byte) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.data[key] = value
}

var cache = NewCache()

func main() {
    http.HandleFunc("/", proxyHandler)
    addr := fmt.Sprintf(":%d", proxyPort)
    log.Printf("Starting proxy server on %s", addr)
    if err := http.ListenAndServe(addr, nil); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
    // Ожидаем URL в формате /example.com/path
    if len(r.URL.Path) < 2 {
        http.Error(w, "No target host specified", http.StatusBadRequest)
        return
    }

    // Извлекаем целевой URL из пути запроса
    targetHost := r.URL.Path[1:]
    targetURL := buildTargetURL(r)

    // Проверка кэша
    if cachedResponse, found := cache.Get(targetURL); found {
        w.Header().Set("Content-Type", "text/html")
        w.Write(cachedResponse)
        return
    }

    // Создаем новый запрос к целевому серверу
    req, err := http.NewRequest(r.Method, targetURL, nil)
    if err != nil {
        http.Error(w, "Failed to create request", http.StatusInternalServerError)
        return
    }

    // Копируем заголовки из исходного запроса, кроме заголовка Host
    copyHeaders(req.Header, r.Header)
    req.Header.Del("Host")

    // Используем стандартный клиент
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        http.Error(w, "Failed to fetch target URL", http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    // Читаем тело ответа
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        http.Error(w, "Failed to read response", http.StatusInternalServerError)
        return
    }

    // Проверяем, является ли контент HTML
    contentType := resp.Header.Get("Content-Type")
    if strings.Contains(contentType, "text/html") {
        modifiedBody, err := modifyHTMLLinks(body, targetHost)
        if err != nil {
            http.Error(w, "Failed to modify HTML", http.StatusInternalServerError)
            return
        }
        body = modifiedBody
        cache.Set(targetURL, body)
    }

    // Копируем заголовки ответа, исключая заголовки, которые могут вызывать проблемы
    copyHeaders(w.Header(), resp.Header)
    w.Header().Del("Content-Length") // Удаляем Content-Length, чтобы избежать несоответствий после изменения тела

    // Устанавливаем новый Content-Length
    w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))

    // Отправляем статус-код и тело
    w.WriteHeader(resp.StatusCode)
    w.Write(body)
}

func buildTargetURL(r *http.Request) string {
    // Извлекаем путь после домена
    path := ""
    if len(r.URL.Path) > 1 {
        path = r.URL.Path[1:]
    }

    // Формируем полный URL
    scheme := "http"
    if r.TLS != nil {
        scheme = "https"
    }
    targetURL := fmt.Sprintf("%s://%s%s", scheme, path, "")
    if r.URL.RawQuery != "" {
        targetURL += "?" + r.URL.RawQuery
    }
    return targetURL
}

func copyHeaders(dst, src http.Header) {
    for key, values := range src {
        for _, value := range values {
            // Исключаем заголовки, которые должны быть уникальными или управляемыми прокси
            if strings.ToLower(key) == "host" {
                continue
            }
            dst.Add(key, value)
        }
    }
}

func modifyHTMLLinks(body []byte, targetHost string) ([]byte, error) {
    doc, err := html.Parse(bytes.NewReader(body))
    if err != nil {
        return nil, err
    }

    var modifyLinksFunc func(*html.Node)
    modifyLinksFunc = func(n *html.Node) {
        if n.Type == html.ElementNode {
            var attrKey string
            if n.Data == "a" {
                attrKey = "href"
            } else if n.Data == "img" || n.Data == "script" || n.Data == "link" {
                attrKey = "src"
            }

            if attrKey != "" {
                for i, attr := range n.Attr {
                    if attr.Key == attrKey {
                        originalURL := attr.Val
                        newURL := rewriteURL(originalURL)
                        if newURL != "" {
                            n.Attr[i].Val = newURL
                        }
                    }
                }
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            modifyLinksFunc(c)
        }
    }

    modifyLinksFunc(doc)

    var buf bytes.Buffer
    err = html.Render(&buf, doc)
    if err != nil {
        return nil, err
    }

    return buf.Bytes(), nil
}

func rewriteURL(originalURL string) string {
    parsedURL, err := url.Parse(originalURL)
    if err != nil {
        return ""
    }

    // Обрабатываем только абсолютные URL
    if parsedURL.IsAbs() {
        // Формируем проксированный URL
        proxyScheme := "http" // Измените на "https", если реализуете HTTPS прокси
        proxyHost := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
        newURL := fmt.Sprintf("%s://%s/%s%s", proxyScheme, proxyHost, parsedURL.Host, parsedURL.Path)
        if parsedURL.RawQuery != "" {
            newURL += "?" + parsedURL.RawQuery
        }
        return newURL
    }

    // Обрабатываем относительные URL
    // Добавляем прокси-сервер перед относительным путем
    if strings.HasPrefix(originalURL, "/") {
        proxyScheme := "http" // Измените на "https", если реализуете HTTPS прокси
        proxyHost := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
        newURL := fmt.Sprintf("%s://%s/%s", proxyScheme, proxyHost, strings.TrimPrefix(originalURL, "/"))
        return newURL
    }

    // Для других случаев (например, якоря) оставляем как есть
    return ""
}
