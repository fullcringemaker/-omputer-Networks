package main

import (
    "fmt"
    "html/template"
    "log"
    "net/http"

    "golang.org/x/net/html"
)

// Шаблон для отображения новостей
var tmpl = `
<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Последние новости с Rusvesna</title>
</head>
<body>
    <h1>Последние новости с сайта Rusvesna</h1>
    {{range .}}
    <hr>
        <div>
            <a href="{{.Link}}" target="_blank">{{.Title}}</a>
    <hr>
        </div>
    {{end}}
</body>
</html>
`

type NewsItem struct {
    Title string
    Link  string
}

func main() {
    http.HandleFunc("/", handler)
    fmt.Println("Server is running at http://185.102.139.169:9742...")
    log.Fatal(http.ListenAndServe("185.102.139.169:9742", nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
    log.Println("Incoming request from client")

    url := "https://rusvesna.su/news"
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        http.Error(w, "Request creation error", http.StatusInternalServerError)
        return
    }
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/85.0.4183.121 Safari/537.36")
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Println("Error fetching news:", err)
        http.Error(w, "Failed to fetch news", http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        log.Println("Non-OK HTTP status:", resp.Status)
        http.Error(w, "Error fetching data", http.StatusInternalServerError)
        return
    }

    doc, err := html.Parse(resp.Body)
    if err != nil {
        log.Println("Error parsing HTML:", err)
        http.Error(w, "Error parsing HTML", http.StatusInternalServerError)
        return
    }

    newsItems := extractNews(doc)

    if len(newsItems) > 64 {
        newsItems = newsItems[:64]
    }

    t := template.New("News Template")
    t, err = t.Parse(tmpl)
    if err != nil {
        log.Println("Error parsing template:", err)
        http.Error(w, "Template error", http.StatusInternalServerError)
        return
    }

    err = t.Execute(w, newsItems)
    if err != nil {
        log.Println("Error executing template:", err)
    }
    log.Println("Request handled successfully")
}

func extractNews(n *html.Node) []NewsItem {
    var news []NewsItem
    var f func(*html.Node)
    f = func(n *html.Node) {
        if n.Type == html.ElementNode && n.Data == "a" {
            var link, title string
            for _, attr := range n.Attr {
                if attr.Key == "href" {
                    link = attr.Val
                }
            }
            if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
                title = n.FirstChild.Data
            }
            if link != "" && title != "" && startsWithNewsLink(link) {
                // Update the link to the absolute URL
                absoluteLink := "https://rusvesna.su" + link
                news = append(news, NewsItem{Title: title, Link: absoluteLink})
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            f(c)
        }
    }
    f(n)
    return news
}

func startsWithNewsLink(link string) bool {
    return len(link) >= 6 && link[:6] == "/news/"
}
