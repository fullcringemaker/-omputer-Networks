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
    ID          int       `json:"ID"`
    Title       string    `json:"Title"`
    Link        string    `json:"Link"`
    Description string    `json:"Description"`
    PubDate     time.Time `json:"PubDate"`
}

var (
    clients            = make(map[*websocket.Conn]bool)
    broadcast          = make(chan []*NewsItem)
    upgrader           = websocket.Upgrader{}
    mutex              = &sync.Mutex{}
    initialPopulation  = false
)

func main() {
    // Подключение к базе данных
    db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs?charset=utf8")
    if err != nil {
        log.Fatal("Ошибка при подключении к базе данных:", err)
    }
    defer db.Close()

    // Настройка пула соединений
    db.SetConnMaxLifetime(time.Minute * 3)
    db.SetMaxOpenConns(10)
    db.SetMaxIdleConns(10)

    // Проверка, пуста ли таблица
    count, err := getNewsCount(db)
    if err != nil {
        log.Fatal("Ошибка при получении количества новостей:", err)
    }

    if count == 0 {
        log.Println("Таблица пуста. Выполняется начальная загрузка новостей...")
        newsItems, err := parseRSS()
        if err != nil {
            log.Fatal("Ошибка при парсинге RSS:", err)
        }
        err = insertInitialNews(db, newsItems)
        if err != nil {
            log.Fatal("Ошибка при вставке начальных новостей:", err)
        }
        log.Println("Начальная загрузка новостей завершена.")
    }

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
        ticker := time.NewTicker(1 * time.Second) // Установка интервала обновления в 1 секунду
        defer ticker.Stop()
        for {
            <-ticker.C
            newsItems, err := parseRSS()
            if err != nil {
                log.Println("Ошибка при парсинге RSS:", err)
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
        }
    }()

    // Запуск сервера
    log.Println("Сервер запущен на порту 9742")
    err = http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatal("Ошибка при запуске сервера:", err)
    }
}

// getNewsCount возвращает количество записей в таблице iu9Trofimenko
func getNewsCount(db *sql.DB) (int, error) {
    var count int
    err := db.QueryRow("SELECT COUNT(*) FROM iu9Trofimenko").Scan(&count)
    if err != nil {
        return 0, err
    }
    return count, nil
}

// insertInitialNews выполняет начальную вставку новостей в таблицу
func insertInitialNews(db *sql.DB, newsItems []*NewsItem) error {
    for _, item := range newsItems {
        // Обработка русских символов
        title := unidecode.Unidecode(item.Title)
        description := unidecode.Unidecode(item.Description)

        _, err := db.Exec("INSERT INTO iu9Trofimenko (title, link, description, pub_date) VALUES (?, ?, ?, ?)",
            title, item.Link, description, item.PubDate.Format("2006-01-02 15:04:05"))
        if err != nil {
            return err
        }
    }
    return nil
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
            Title:       item.Title,
            Link:        item.Link,
            Description: item.Description,
            PubDate:     pubDate,
        }
        newsItems = append(newsItems, newsItem)
    }
    return newsItems, nil
}

func updateDatabase(db *sql.DB, newsItems []*NewsItem) error {
    mutex.Lock()
    defer mutex.Unlock()

    // Проверка количества записей в таблице
    var count int
    err := db.QueryRow("SELECT COUNT(*) FROM iu9Trofimenko").Scan(&count)
    if err != nil {
        return err
    }

    if count == 0 {
        // Таблица пуста и начальная загрузка уже выполнена, не вставляем снова
        return nil
    }

    for _, item := range newsItems {
        // Обработка русских символов
        title := unidecode.Unidecode(item.Title)
        description := unidecode.Unidecode(item.Description)

        var id int
        err := db.QueryRow("SELECT id FROM iu9Trofimenko WHERE link = ?", item.Link).Scan(&id)
        if err == sql.ErrNoRows {
            // Новость отсутствует, вставляем
            _, err = db.Exec("INSERT INTO iu9Trofimenko (title, link, description, pub_date) VALUES (?, ?, ?, ?)",
                title, item.Link, description, item.PubDate.Format("2006-01-02 15:04:05"))
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
            if dbTitle != title || dbDescription != description {
                // Обновляем запись
                _, err = db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, pub_date = ? WHERE id = ?",
                    title, description, item.PubDate.Format("2006-01-02 15:04:05"), id)
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
        var pubDateStr string
        err := rows.Scan(&item.ID, &item.Title, &item.Link, &item.Description, &pubDateStr)
        if err != nil {
            return nil, err
        }
        pubDate, err := time.Parse("2006-01-02 15:04:05", pubDateStr)
        if err != nil {
            pubDate = time.Now()
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
