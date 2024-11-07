package main

import (
    "database/sql"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/mmcdole/gofeed"
    _ "github.com/go-sql-driver/mysql"
    "encoding/json"
)

const (
    dbUser     = "iu9networkslabs"
    dbPassword = "Je2dTYr6"
    dbName     = "iu9networkslabs"
    dbHost     = "students.yss.su"
    tableName  = "iu9Trofimenko"
    rssURL     = "https://rospotrebnadzor.ru/region/rss/rss.php?rss=y"
)

// Структура для хранения новости
type NewsItem struct {
    ID          int
    Title       string
    Description string
    Date        string
}

func main() {
    // Подключаемся к базе данных
    dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8", dbUser, dbPassword, dbHost, dbName)
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        log.Fatalf("Ошибка подключения к базе данных: %v", err)
    }
    defer db.Close()

    // Проверяем подключение
    if err = db.Ping(); err != nil {
        log.Fatalf("База данных недоступна: %v", err)
    }

    // Создаем таблицу, если она не существует
    createTable(db)

    // Запускаем парсинг RSS каждые 5 минут в отдельной горутине
    go func() {
        for {
            parseAndUpdate(db)
            time.Sleep(5 * time.Minute)
        }
    }()

    // Запускаем веб-сервер
    http.HandleFunc("/", dashboardHandler)
    http.HandleFunc("/news", newsHandler(db))
    fmt.Println("Сервер запущен на порту 8080...")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

// Функция для создания таблицы
func createTable(db *sql.DB) {
    query := fmt.Sprintf(`
        CREATE TABLE IF NOT EXISTS %s (
            id INT AUTO_INCREMENT PRIMARY KEY,
            title VARCHAR(255),
            description TEXT,
            date VARCHAR(10)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8;
    `, tableName)

    _, err := db.Exec(query)
    if err != nil {
        log.Fatalf("Ошибка создания таблицы: %v", err)
    }
}

// Функция для парсинга RSS и обновления базы данных
func parseAndUpdate(db *sql.DB) {
    fmt.Println("Начало парсинга RSS...")

    // Парсим RSS-ленту
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL(rssURL)
    if err != nil {
        log.Printf("Ошибка парсинга RSS: %v", err)
        return
    }

    for _, item := range feed.Items {
        // Форматируем дату
        pubDate := item.PublishedParsed.Format("02.01.2006")

        // Проверяем, есть ли запись в базе данных
        exists, err := checkIfExists(db, item.Title)
        if err != nil {
            log.Printf("Ошибка проверки существования записи: %v", err)
            continue
        }

        if exists {
            // Обновляем запись, если необходимо
            err = updateRecord(db, item.Title, item.Description, pubDate)
            if err != nil {
                log.Printf("Ошибка обновления записи: %v", err)
            }
        } else {
            // Вставляем новую запись
            err = insertRecord(db, item.Title, item.Description, pubDate)
            if err != nil {
                log.Printf("Ошибка вставки записи: %v", err)
            }
        }
    }

    fmt.Println("Парсинг RSS завершен.")
}

// Функция для проверки существования записи
func checkIfExists(db *sql.DB, title string) (bool, error) {
    query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE title = ?", tableName)
    var count int
    err := db.QueryRow(query, title).Scan(&count)
    if err != nil {
        return false, err
    }
    return count > 0, nil
}

// Функция для обновления записи
func updateRecord(db *sql.DB, title, description, date string) error {
    query := fmt.Sprintf("UPDATE %s SET description = ?, date = ? WHERE title = ?", tableName)
    _, err := db.Exec(query, description, date, title)
    return err
}

// Функция для вставки новой записи
func insertRecord(db *sql.DB, title, description, date string) error {
    query := fmt.Sprintf("INSERT INTO %s (title, description, date) VALUES (?, ?, ?)", tableName)
    _, err := db.Exec(query, title, description, date)
    return err
}

// Обработчик для отображения дэшборда
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "dashboard.html")
}

// Обработчик для предоставления новостей в формате JSON
func newsHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        newsItems, err := fetchNews(db)
        if err != nil {
            http.Error(w, "Ошибка получения новостей", http.StatusInternalServerError)
            return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(newsItems)
    }
}

// Функция для получения новостей из базы данных
func fetchNews(db *sql.DB) ([]NewsItem, error) {
    query := fmt.Sprintf("SELECT id, title, description, date FROM %s ORDER BY id DESC", tableName)
    rows, err := db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var newsItems []NewsItem
    for rows.Next() {
        var item NewsItem
        err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Date)
        if err != nil {
            return nil, err
        }
        newsItems = append(newsItems, item)
    }

    return newsItems, nil
}
