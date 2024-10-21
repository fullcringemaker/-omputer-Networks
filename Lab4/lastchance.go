// proxy.go
package main

import (
    "bufio"
    "bytes"
    "fmt"
    "golang.org/x/net/html"
    "io/ioutil"
    "net/http"
    "net/url"
    "strings"
    "sync"
    "time"
)

const (
    PROXY_PORT = "9742" // Порт, на котором будет работать прокси
    CERT_FILE  = "cert.pem"
    KEY_FILE   = "key.pem"
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
    // Запуск HTTPS-сервера с использованием самоподписанного сертификата и ключа
    err := http.ListenAndServeTLS(":"+PROXY_PORT, CERT_FILE, KEY_FILE, nil)
    if err != nil {
        // Если произошла ошибка при запуске сервера, программа завершится
        panic(err)
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

    // Проверяем кэш
    if cachedResponse, found := cache.Get(targetURL); found {
        w.Write(cachedResponse)
        return
    }

    // Создаем новый запрос к целевому серверу
    req, err := http.NewRequest(r.Method, targetURL, r.Body)
    if err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    // Копируем заголовки из исходного запроса
    copyHeaders(r.Header, req.Header)

    client := &http.Client{
        Timeout: 15 * time.Second,
    }

    resp, err := client.Do(req)
    if err != nil {
        http.Error(w, "Error fetching the requested page", http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    // Обработка перенаправлений (редиректов)
    if resp.StatusCode >= 300 && resp.StatusCode < 400 {
        location := resp.Header.Get("Location")
        if location != "" {
            newLocation, err := rewriteRedirectLocation(location, scheme, r.Host)
            if err == nil {
                w.Header().Set("Location", newLocation)
            }
        }
    }

    // Копируем заголовки ответа, кроме заголовков, связанных с Content-Length и Transfer-Encoding
    // Так как мы можем изменить тело ответа
    copyHeaders(resp.Header, w.Header())

    // Если контент HTML, парсим и изменяем ссылки
    contentType := resp.Header.Get("Content-Type")
    if strings.Contains(contentType, "text/html") {
        bodyBytes, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            http.Error(w, "Error reading response body", http.StatusInternalServerError)
            return
        }

        // Формируем базовый URL для тега <base>
        baseURL := fmt.Sprintf("%s://%s/%s/", scheme, r.Host, domain)

        modifiedBody, err := rewriteHTML(bodyBytes, domain, baseURL)
        if err != nil {
            http.Error(w, "Error parsing HTML", http.StatusInternalServerError)
            return
        }

        // Сохраняем в кэш
        cache.Set(targetURL, modifiedBody)

        w.Header().Set("Content-Length", fmt.Sprintf("%d", len(modifiedBody)))
        w.Write(modifiedBody)
    } else {
        // Для других типов контента просто проксируем
        bodyBytes, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            http.Error(w, "Error reading response body", http.StatusInternalServerError)
            return
        }

        // Сохраняем в кэш
        cache.Set(targetURL, bodyBytes)

        w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
        w.Write(bodyBytes)
    }
}

func copyHeaders(src http.Header, dest http.Header) {
    for key, values := range src {
        // Исключаем заголовки, которые могут быть изменены прокси
        if key == "Content-Length" || key == "Transfer-Encoding" {
            continue
        }
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
                // Для тегов link, обрабатываем href
                attr = "href"
            }

            if attr != "" {
                for i, a := range n.Attr {
                    if a.Key == attr {
                        originalURL := a.Val
                        newURL := rewriteURL(originalURL, domain)
                        if newURL != originalURL {
                            n.Attr[i].Val = newURL
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
        return originalURL
    }

    // Если URL абсолютный, переписываем через прокси
    if parsedURL.IsAbs() {
        // Сохраняем путь, параметры и фрагмент
        newURL := fmt.Sprintf("/%s%s", parsedURL.Host, parsedURL.Path)
        if parsedURL.RawQuery != "" {
            newURL += "?" + parsedURL.RawQuery
        }
        if parsedURL.Fragment != "" {
            newURL += "#" + parsedURL.Fragment
        }
        return newURL
    }

    // Если URL относительный, начинающийся с '/', переписываем через прокси
    if strings.HasPrefix(originalURL, "/") {
        newURL := fmt.Sprintf("/%s%s", domain, originalURL)
        return newURL
    }

    // Относительный URL, не начинающийся с '/', оставляем как есть
    return originalURL
}

func rewriteRedirectLocation(location string, scheme string, proxyHost string) (string, error) {
    parsedLocation, err := url.Parse(location)
    if err != nil {
        return "", err
    }

    if parsedLocation.IsAbs() {
        // Переписываем абсолютный URL через прокси
        newLocation := fmt.Sprintf("%s://%s/%s%s", scheme, proxyHost, parsedLocation.Host, parsedLocation.Path)
        if parsedLocation.RawQuery != "" {
            newLocation += "?" + parsedLocation.RawQuery
        }
        if parsedLocation.Fragment != "" {
            newLocation += "#" + parsedLocation.Fragment
        }
        return newLocation, nil
    } else if strings.HasPrefix(location, "/") {
        // Переписываем относительный URL через прокси
        newLocation := fmt.Sprintf("/%s%s", proxyHost, location)
        return newLocation, nil
    }

    // Протокол-независимые URL или другие относительные URL оставляем как есть
    return location, nil
}
