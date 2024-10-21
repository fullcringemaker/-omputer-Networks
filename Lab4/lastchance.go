// proxy.go
package main

import (
    "bufio"
    "bytes"
    "fmt"
    "golang.org/x/net/html"
    "io"
    "io/ioutil"
    "log"
    "net/http"
    "net/url"
    "strings"
    "sync"
    "time"
)

const (
    PROXY_PORT = "9742" // Порт, на котором будет работать прокси
)

// Простая структура для кэша
type Cache struct {
    mu    sync.Mutex
    items map[string][]byte
}

func NewCache() *Cache {
    return &Cache{
        items: make(map[string][]byte),
    }
}

func (c *Cache) Get(key string) ([]byte, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    val, exists := c.items[key]
    return val, exists
}

func (c *Cache) Set(key string, value []byte) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = value
}

var cache = NewCache()

func main() {
    http.HandleFunc("/", handleRequestAndRedirect)
    log.Printf("Starting proxy server on port %s...\n", PROXY_PORT)
    if err := http.ListenAndServe(":"+PROXY_PORT, nil); err != nil {
        log.Fatal("ListenAndServe:", err)
    }
}

func handleRequestAndRedirect(w http.ResponseWriter, r *http.Request) {
    // Извлекаем домен из URL-пути
    // Ожидаемый формат: /domain.com/...
    path := strings.TrimPrefix(r.URL.Path, "/")
    parts := strings.SplitN(path, "/", 2)
    domain := parts[0]
    var newPath string
    if len(parts) > 1 {
        newPath = "/" + parts[1]
    } else {
        newPath = "/"
    }

    // Проверяем, если исходный запрос не заканчивается на '/', перенаправляем на URL с '/'
    if !strings.HasSuffix(r.URL.Path, "/") && newPath == "/" {
        newURL := r.URL.Path + "/"
        if r.URL.RawQuery != "" {
            newURL += "?" + r.URL.RawQuery
        }
        http.Redirect(w, r, newURL, http.StatusMovedPermanently)
        return
    }

    // Определяем схему (http или https)
    scheme := "http"
    if r.TLS != nil {
        scheme = "https"
    }

    targetURL := fmt.Sprintf("%s://%s%s", scheme, domain, newPath)
    log.Printf("Proxying request to: %s", targetURL)

    // Проверяем кэш
    if cachedResponse, found := cache.Get(targetURL); found {
        log.Printf("Cache hit for: %s", targetURL)
        w.Write(cachedResponse)
        return
    }

    // Создаем новый запрос
    req, err := http.NewRequest(r.Method, targetURL, r.Body)
    if err != nil {
        log.Printf("Error creating request: %v", err)
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    // Копируем заголовки
    copyHeaders(r.Header, req.Header)

    client := &http.Client{
        Timeout: 15 * time.Second,
    }

    resp, err := client.Do(req)
    if err != nil {
        log.Printf("Error fetching URL %s: %v", targetURL, err)
        http.Error(w, "Error fetching the requested page", http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    // Копируем заголовки ответа
    copyHeaders(resp.Header, w.Header())

    // Если контент HTML, парсим и изменяем ссылки
    contentType := resp.Header.Get("Content-Type")
    if strings.Contains(contentType, "text/html") {
        bodyBytes, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            log.Printf("Error reading response body: %v", err)
            http.Error(w, "Error reading response body", http.StatusInternalServerError)
            return
        }

        // Формируем базовый URL для тега <base>
        baseURL := fmt.Sprintf("%s://%s/%s/", scheme, r.Host, domain)

        modifiedBody, err := rewriteHTML(bodyBytes, domain, baseURL)
        if err != nil {
            log.Printf("Error parsing HTML: %v", err)
            http.Error(w, "Error parsing HTML", http.StatusInternalServerError)
            return
        }

        // Сохраняем в кэш
        cache.Set(targetURL, modifiedBody)

        w.Write(modifiedBody)
    } else {
        // Для других типов контента просто проксируем
        bodyBytes, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            log.Printf("Error reading non-HTML response body: %v", err)
            http.Error(w, "Error reading response body", http.StatusInternalServerError)
            return
        }

        // Сохраняем в кэш
        cache.Set(targetURL, bodyBytes)

        w.Write(bodyBytes)
    }
}

func copyHeaders(src http.Header, dest http.Header) {
    for key, values := range src {
        for _, value := range values {
            dest.Add(key, value)
        }
    }
}

func rewriteHTML(body []byte, domain string, baseURL string) ([]byte, error) {
    doc, err := html.Parse(bytes.NewReader(body))
    if err != nil {
        return nil, err
    }

    var f func(*html.Node)
    f = func(n *html.Node) {
        if n.Type == html.ElementNode {
            var attr string
            if n.Data == "a" {
                attr = "href"
            } else if n.Data == "img" || n.Data == "script" {
                attr = "src"
            } else if n.Data == "link" {
                attr = "href"
            }

            if attr != "" {
                for i, a := range n.Attr {
                    if a.Key == attr {
                        originalURL := a.Val
                        newURL := rewriteURL(originalURL, domain)
                        if newURL != originalURL {
                            n.Attr[i].Val = newURL
                            log.Printf("Rewrote %s: %s -> %s", attr, originalURL, newURL)
                        }
                    }
                }
            }

            // Добавляем тег <base> в <head>
            if n.Data == "head" {
                // Проверяем, есть ли уже тег <base>
                hasBase := false
                for c := n.FirstChild; c != nil; c = c.NextSibling {
                    if c.Type == html.ElementNode && c.Data == "base" {
                        hasBase = true
                        break
                    }
                }
                if !hasBase {
                    baseNode := &html.Node{
                        Type: html.ElementNode,
                        Data: "base",
                        Attr: []html.Attribute{
                            {
                                Key: "href",
                                Val: baseURL,
                            },
                        },
                    }
                    n.AppendChild(baseNode)
                    log.Printf("Added <base href=\"%s\">", baseURL)
                }
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            f(c)
        }
    }

    f(doc)

    var buf bytes.Buffer
    writer := bufio.NewWriter(&buf)
    err = html.Render(writer, doc)
    if err != nil {
        return nil, err
    }
    writer.Flush()
    return buf.Bytes(), nil
}

func rewriteURL(originalURL string, domain string) string {
    // Обработка пустых и невалидных URL
    if originalURL == "" || strings.HasPrefix(originalURL, "javascript:") || strings.HasPrefix(originalURL, "mailto:") {
        return originalURL
    }

    parsedURL, err := url.Parse(originalURL)
    if err != nil {
        log.Printf("Error parsing URL %s: %v", originalURL, err)
        return originalURL
    }

    // Если URL относительный, начинающийся с '/', переписываем через прокси
    if parsedURL.IsAbs() {
        // Абсолютный URL
        newURL := fmt.Sprintf("/%s%s", parsedURL.Host, parsedURL.Path)
        if parsedURL.RawQuery != "" {
            newURL += "?" + parsedURL.RawQuery
        }
        if parsedURL.Fragment != "" {
            newURL += "#" + parsedURL.Fragment
        }
        return newURL
    } else if strings.HasPrefix(originalURL, "/") {
        // Относительный URL, начинающийся с '/'
        newURL := fmt.Sprintf("/%s%s", domain, originalURL)
        return newURL
    }

    // Относительный URL, не начинающийся с '/', оставляем как есть
    return originalURL
}
