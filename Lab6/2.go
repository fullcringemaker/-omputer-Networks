package main

import (
    "database/sql"
    "fmt"
    "html/template"
    "log"
    "net/http"
    "sync"
    "time"

    "github.com/gorilla/websocket"
    _ "github.com/go-sql-driver/mysql"
    "github.com/mmcdole/gofeed"
    "github.com/rainycape/unidecode"
)

type NewsItem struct {
    ID          int
    Title       string
    Link        string
    Description string
    PubDate     time.Time
}

var (
    clients   = make(map[*websocket.Conn]bool)
    broadcast = make(chan []*NewsItem)
    upgrader  = websocket.Upgrader{}
    mutex     = &sync.Mutex{}
)

func main() {
    // Подключение к базе данных
    db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs?charset=utf8")
    if err != nil {
        log.Fatal("Ошибка при подключении к базе данных:", err)
    }
    defer db.Close()

    // Установка кодировки для корректного отображения русских символов
    db.SetConnMaxLifetime(time.Minute * 3)
    db.SetMaxOpenConns(10)
    db.SetMaxIdleConns(10)

    // Запуск горутины для обработки сообщений WebSocket
    go handleMessages()

    // Маршруты
    http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        handleConnections(w, r, db)
    })
    http.HandleFunc("/parser", func(w http.ResponseWriter, r *http.Request) {
        tmpl := template.Must(template.ParseFiles("parser.html"))
        newsItems, err := fetchNewsFromDB(db)
        if err != nil {
            log.Println("Ошибка при получении данных из базы:", err)
            http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
            return
        }
        tmpl.Execute(w, newsItems)
    })
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "dashboard.html")
    })

    // Запуск периодического обновления данных
    go func() {
        for {
            newsItems, err := parseRSS()
            if err != nil {
                log.Println("Ошибка при парсинге RSS:", err)
                time.Sleep(10 * time.Second)
                continue
            }
            err = updateDatabase(db, newsItems)
            if err != nil {
                log.Println("Ошибка при обновлении базы данных:", err)
            }

            updatedNews, err := fetchNewsFromDB(db)
            if err != nil {
                log.Println("Ошибка при получении данных из базы:", err)
            } else {
                broadcast <- updatedNews
            }

            time.Sleep(10 * time.Second)
        }
    }()

    // Запуск сервера
    log.Println("Сервер запущен на порту 9742")
    err = http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatal("Ошибка при запуске сервера:", err)
    }
}

func parseRSS() ([]*NewsItem, error) {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL("https://ldpr.ru/rss")
    if err != nil {
        return nil, err
    }

    var newsItems []*NewsItem
    for _, item := range feed.Items {
        pubDate, err := time.Parse(time.RFC1123Z, item.Published)
        if err != nil {
            pubDate = time.Now()
        }

        newsItem := &NewsItem{
            Title:       unidecode.Unidecode(item.Title),
            Link:        item.Link,
            Description: unidecode.Unidecode(item.Description),
            PubDate:     pubDate,
        }
        newsItems = append(newsItems, newsItem)
    }
    return newsItems, nil
}

func updateDatabase(db *sql.DB, newsItems []*NewsItem) error {
    for _, item := range newsItems {
        var id int
        err := db.QueryRow("SELECT id FROM iu9Trofimenko WHERE link = ?", item.Link).Scan(&id)
        if err == sql.ErrNoRows {
            // Новость отсутствует, вставляем
            _, err = db.Exec("INSERT INTO iu9Trofimenko (title, link, description, pub_date) VALUES (?, ?, ?, ?)",
                item.Title, item.Link, item.Description, item.PubDate)
            if err != nil {
                return err
            }
        } else if err != nil {
            return err
        } else {
            // Проверяем на изменения
            var dbTitle, dbDescription string
            err := db.QueryRow("SELECT title, description FROM iu9Trofimenko WHERE id = ?", id).Scan(&dbTitle, &dbDescription)
            if err != nil {
                return err
            }
            if dbTitle != item.Title || dbDescription != item.Description {
                // Обновляем запись
                _, err = db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, pub_date = ? WHERE id = ?",
                    item.Title, item.Description, item.PubDate, id)
                if err != nil {
                    return err
                }
            }
        }
    }
    return nil
}

func fetchNewsFromDB(db *sql.DB) ([]*NewsItem, error) {
    rows, err := db.Query("SELECT id, title, link, description, pub_date FROM iu9Trofimenko ORDER BY pub_date DESC")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var newsItems []*NewsItem
    for rows.Next() {
        var item NewsItem
        var pubDate time.Time
        err := rows.Scan(&item.ID, &item.Title, &item.Link, &item.Description, &pubDate)
        if err != nil {
            return nil, err
        }
        item.PubDate = pubDate
        newsItems = append(newsItems, &item)
    }
    return newsItems, nil
}

func handleConnections(w http.ResponseWriter, r *http.Request, db *sql.DB) {
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Ошибка при обновлении до WebSocket:", err)
        return
    }
    defer ws.Close()

    mutex.Lock()
    clients[ws] = true
    mutex.Unlock()

    // Отправляем текущие данные новостей новому клиенту
    newsItems, err := fetchNewsFromDB(db)
    if err != nil {
        log.Println("Ошибка при получении данных из базы:", err)
    } else {
        err = ws.WriteJSON(newsItems)
        if err != nil {
            log.Println("Ошибка при отправке данных через WebSocket:", err)
        }
    }

    for {
        // Ожидаем сообщений от клиента (не используем, но нужно для поддержки соединения)
        _, _, err := ws.ReadMessage()
        if err != nil {
            mutex.Lock()
            delete(clients, ws)
            mutex.Unlock()
            break
        }
    }
}

func handleMessages() {
    for {
        newsItems := <-broadcast
        mutex.Lock()
        for client := range clients {
            err := client.WriteJSON(newsItems)
            if err != nil {
                log.Println("Ошибка при отправке данных через WebSocket:", err)
                client.Close()
                delete(clients, client)
            }
        }
        mutex.Unlock()
    }
}
