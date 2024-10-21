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
    log.Printf("Запуск прокси-сервера на %s", addr)
    if err := http.ListenAndServe(addr, nil); err != nil {
        log.Fatalf("Не удалось запустить сервер: %v", err)
    }
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
    // Ожидаем URL в формате /example.com/path
    if len(r.URL.Path) < 2 {
        http.Error(w, "Не указан целевой хост", http.StatusBadRequest)
        return
    }

    // Извлекаем целевой URL из пути запроса
    targetHost := r.URL.Path[1:]
    targetURL := buildTargetURL(r, targetHost)

    // Проверка кэша
    if cachedResponse, found := cache.Get(targetURL); found {
        w.Header().Set("Content-Type", "text/html")
        w.Write(cachedResponse)
        return
    }

    // Создаем новый запрос к целевому серверу
    req, err := http.NewRequest(r.Method, targetURL, nil)
    if err != nil {
        http.Error(w, "Не удалось создать запрос", http.StatusInternalServerError)
        return
    }

    // Копируем заголовки из исходного запроса, кроме заголовка Host
    copyHeaders(req.Header, r.Header)
    req.Header.Del("Host")

    // Используем стандартный клиент
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        http.Error(w, "Не удалось получить целевой URL", http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    // Читаем тело ответа
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        http.Error(w, "Не удалось прочитать ответ", http.StatusInternalServerError)
        return
    }

    // Проверяем, является ли контент HTML
    contentType := resp.Header.Get("Content-Type")
    if strings.Contains(contentType, "text/html") {
        modifiedBody, err := modifyHTMLLinks(body, targetHost)
        if err != nil {
            http.Error(w, "Не удалось модифицировать HTML", http.StatusInternalServerError)
            return
        }
        body = modifiedBody
        cache.Set(targetURL, body)
    }

    // Копируем заголовки ответа, исключая заголовки, которые могут вызвать проблемы
    copyHeaders(w.Header(), resp.Header)
    w.Header().Del("Content-Length") // Удаляем Content-Length, чтобы избежать несоответствий после изменения тела

    // Устанавливаем новый Content-Length
    w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))

    // Отправляем статус-код и тело
    w.WriteHeader(resp.StatusCode)
    w.Write(body)
}

func buildTargetURL(r *http.Request, targetHost string) string {
    // Формируем полный URL
    scheme := "http"
    if r.TLS != nil {
        scheme = "https"
    }
    // В текущей реализации поддерживается только HTTP
    targetURL := fmt.Sprintf("http://%s%s", targetHost, r.URL.RawPath)
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

    modifyLinksFunc := func(n *html.Node) {
        if n.Type == html.ElementNode {
            // Список атрибутов, содержащих URL
            urlAttrs := map[string][]string{
                "a":       {"href"},
                "img":     {"src", "srcset"},
                "script":  {"src"},
                "link":    {"href"},
                "form":    {"action"},
                "iframe":  {"src"},
                "video":   {"src", "poster"},
                "audio":   {"src"},
                "source":  {"src", "srcset"},
                "embed":   {"src"},
                "object":  {"data"},
                "button":  {"formaction"},
                "input":   {"formaction"},
                "base":    {"href"},
                "meta":    {"content"}, // для meta refresh
            }

            attrs, exists := urlAttrs[n.Data]
            if exists {
                for _, attr := range attrs {
                    for i, a := range n.Attr {
                        if a.Key == attr {
                            newURL, err := rewriteURL(a.Val, targetHost)
                            if err == nil && newURL != "" {
                                n.Attr[i].Val = newURL
                            }
                        }
                    }
                }
            }

            // Специальная обработка для атрибута srcset
            if n.Data == "img" || n.Data == "source" {
                for i, a := range n.Attr {
                    if a.Key == "srcset" {
                        newSrcset := processSrcset(a.Val, targetHost)
                        n.Attr[i].Val = newSrcset
                    }
                }
            }

            // Специальная обработка для meta refresh
            if n.Data == "meta" {
                for i, a := range n.Attr {
                    if a.Key == "http-equiv" && strings.ToLower(a.Val) == "refresh" {
                        for j, attr := range n.Attr {
                            if attr.Key == "content" {
                                parts := strings.Split(attr.Val, ";")
                                if len(parts) == 2 {
                                    urlPart := strings.TrimSpace(parts[1])
                                    if strings.HasPrefix(urlPart, "url=") || strings.HasPrefix(urlPart, "URL=") {
                                        originalURL := strings.TrimPrefix(urlPart, "url=")
                                        originalURL = strings.TrimPrefix(originalURL, "URL=")
                                        newURL, err := rewriteURL(originalURL, targetHost)
                                        if err == nil && newURL != "" {
                                            n.Attr[j].Val = fmt.Sprintf("url=%s", newURL)
                                        }
                                    }
                                }
                            }
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

func rewriteURL(originalURL, targetHost string) (string, error) {
    parsedURL, err := url.Parse(originalURL)
    if err != nil {
        return "", err
    }

    // Обрабатываем только абсолютные и относительные URL
    if parsedURL.IsAbs() {
        // Формируем проксированный URL
        proxyScheme := "http" // Измените на "https", если реализуете HTTPS прокси
        proxyHost := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
        newURL := fmt.Sprintf("%s://%s/%s%s", proxyScheme, proxyHost, parsedURL.Host, parsedURL.Path)
        if parsedURL.RawQuery != "" {
            newURL += "?" + parsedURL.RawQuery
        }
        if parsedURL.Fragment != "" {
            newURL += "#" + parsedURL.Fragment
        }
        return newURL, nil
    }

    if strings.HasPrefix(originalURL, "/") {
        // Абсолютный относительный URL
        proxyScheme := "http"
        proxyHost := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
        newURL := fmt.Sprintf("%s://%s/%s", proxyScheme, proxyHost, strings.TrimPrefix(originalURL, "/"))
        return newURL, nil
    }

    if strings.HasPrefix(originalURL, "//") {
        // Протокол-независимый URL
        proxyScheme := "http"
        proxyHost := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
        newURL := fmt.Sprintf("%s://%s/%s", proxyScheme, proxyHost, strings.TrimPrefix(originalURL, "//"))
        return newURL, nil
    }

    // Относительный URL
    proxyScheme := "http"
    proxyHost := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
    newURL := fmt.Sprintf("%s://%s/%s/%s", proxyScheme, proxyHost, targetHost, originalURL)
    return newURL, nil
}

func processSrcset(srcset, targetHost string) string {
    parts := strings.Split(srcset, ",")
    for i, part := range parts {
        subparts := strings.Fields(strings.TrimSpace(part))
        if len(subparts) > 0 {
            originalURL := subparts[0]
            newURL, err := rewriteURL(originalURL, targetHost)
            if err == nil && newURL != "" {
                subparts[0] = newURL
                parts[i] = strings.Join(subparts, " ")
            }
        }
    }
    return strings.Join(parts, ", ")
}
