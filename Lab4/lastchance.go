// proxy.go
package main

import (
    "bytes"
    "fmt"
    "github.com/elazarl/goproxy"
    "golang.org/x/net/html"
    "io"
    "io/ioutil"
    "net/http"
    "net/url"
    "strings"
    "sync"
    "time"
)

const (
    PROXY_PORT = "9742"          // Порт, на котором будет работать прокси
    CERT_FILE  = "cert.pem"      // Путь к сертификату
    KEY_FILE   = "key.pem"       // Путь к приватному ключу
    SERVER_IP  = "185.104.251.226" // IP вашего WDS сервера
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
    proxy := goproxy.NewProxyHttpServer()
    proxy.Verbose = false // Отключаем стандартное логирование

    // Обработчик ответов
    proxy.OnResponse().DoFunc(
        func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
            // Проверяем, является ли контент HTML
            contentType := resp.Header.Get("Content-Type")
            if strings.Contains(contentType, "text/html") {
                bodyBytes, err := ioutil.ReadAll(resp.Body)
                if err != nil {
                    return resp
                }
                resp.Body.Close()

                modifiedBody, err := rewriteHTML(bodyBytes, ctx.Req.URL.Host)
                if err != nil {
                    return resp
                }

                // Сохраняем в кэш
                cache.Set(ctx.Req.URL.String(), modifiedBody)

                // Обновляем тело ответа
                resp.Body = io.NopCloser(bytes.NewBuffer(modifiedBody))
                resp.ContentLength = int64(len(modifiedBody))
                resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(modifiedBody)))
            } else if resp.StatusCode >= 300 && resp.StatusCode < 400 {
                // Обрабатываем редиректы
                location := resp.Header.Get("Location")
                if location != "" {
                    newLocation, err := rewriteRedirectLocation(location, ctx.Req.URL.Host)
                    if err == nil {
                        resp.Header.Set("Location", newLocation)
                    }
                }
            } else {
                // Кэшируем другие типы контента
                bodyBytes, err := ioutil.ReadAll(resp.Body)
                if err != nil {
                    return resp
                }
                resp.Body.Close()

                cache.Set(ctx.Req.URL.String(), bodyBytes)

                // Обновляем тело ответа
                resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
                resp.ContentLength = int64(len(bodyBytes))
                resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
            }

            return resp
        },
    )

    // Обработчик запросов для перенаправлений
    proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

    // Запуск HTTPS-прокси-сервера
    httpServer := &http.Server{
        Addr:         ":" + PROXY_PORT,
        Handler:      proxy,
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
    }

    // Запуск HTTPS-сервера
    httpServer.ListenAndServeTLS(CERT_FILE, KEY_FILE)
}

// Функция для переписывания HTML-контента
func rewriteHTML(body []byte, domain string) ([]byte, error) {
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
                    baseURL := fmt.Sprintf("https://%s:%s/", SERVER_IP, PROXY_PORT)
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

// Функция для переписывания URL
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
        // Формируем новый URL через прокси
        newURL := fmt.Sprintf("https://%s:%s/%s%s", SERVER_IP, PROXY_PORT, parsedURL.Host, parsedURL.Path)
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
        newURL := fmt.Sprintf("https://%s:%s/%s%s", SERVER_IP, PROXY_PORT, domain, originalURL)
        return newURL
    }

    // Относительный URL, не начинающийся с '/', оставляем как есть
    return originalURL
}

// Функция для переписывания заголовка Location в редиректах
func rewriteRedirectLocation(location string, domain string) (string, error) {
    parsedLocation, err := url.Parse(location)
    if err != nil {
        return "", err
    }

    if parsedLocation.IsAbs() {
        // Переписываем абсолютный URL через прокси
        newLocation := fmt.Sprintf("https://%s:%s/%s%s", SERVER_IP, PROXY_PORT, parsedLocation.Host, parsedLocation.Path)
        if parsedLocation.RawQuery != "" {
            newLocation += "?" + parsedLocation.RawQuery
        }
        if parsedLocation.Fragment != "" {
            newLocation += "#" + parsedLocation.Fragment
        }
        return newLocation, nil
    } else if strings.HasPrefix(location, "/") {
        // Переписываем относительный URL через прокси
        newLocation := fmt.Sprintf("https://%s:%s/%s%s", SERVER_IP, PROXY_PORT, domain, location)
        return newLocation, nil
    }

    // Протокол-независимые URL или другие относительные URL оставляем как есть
    return location, nil
}
