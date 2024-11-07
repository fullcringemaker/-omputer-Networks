package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mmcdole/gofeed"
	_ "github.com/go-sql-driver/mysql"
)

// Структура для представления новости
type News struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Date        string `json:"date"`
	Link        string `json:"link"`
	GUID        string `json:"guid"`
}

// Глобальные переменные для управления WebSocket
var (
	clients      = make(map[*websocket.Conn]bool)
	clientsMutex sync.Mutex
	broadcast    = make(chan []News)
	upgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Разрешить все источники. В продакшене стоит ограничить.
		},
	}
)

// Настройки подключения к базе данных
const (
	dbUser     = "iu9networkslabs"
	dbPassword = "Je2dTYr6"
	dbName     = "iu9networkslabs"
	dbHost     = "students.yss.su"
	dbPort     = "3306" // Стандартный порт MySQL
)

// RSS-лента
const rssURL = "https://rospotrebnadzor.ru/region/rss/rss.php?rss=y"

// Функция для установки соединения с базой данных
func setupDatabase() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		dbUser, dbPassword, dbHost, dbPort, dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	// Проверка соединения
	err = db.Ping()
	if err != nil {
		return nil, err
	}

	// Создание таблицы, если она не существует
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS iu9Trofimenko (
		id INT AUTO_INCREMENT PRIMARY KEY,
		title VARCHAR(512) NOT NULL,
		description TEXT,
		date VARCHAR(20),
		link VARCHAR(512),
		guid VARCHAR(512) UNIQUE
	);`
	_, err = db.Exec(createTableQuery)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Функция для парсинга RSS и обновления базы данных
func parseAndUpdate(db *sql.DB) ([]News, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(rssURL)
	if err != nil {
		return nil, err
	}

	var newNews []News

	for _, item := range feed.Items {
		// Проверка на существование по GUID
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM iu9Trofimenko WHERE guid=?)", item.GUID).Scan(&exists)
		if err != nil {
			log.Println("Ошибка проверки существования новости:", err)
			continue
		}

		if !exists {
			// Форматирование даты
			var formattedDate string
			if item.PublishedParsed != nil {
				formattedDate = item.PublishedParsed.Format("02.01.2006")
			} else {
				formattedDate = ""
			}

			// Вставка новой новости
			insertQuery := `INSERT INTO iu9Trofimenko (title, description, date, link, guid) VALUES (?, ?, ?, ?, ?)`
			_, err := db.Exec(insertQuery, item.Title, item.Description, formattedDate, item.Link, item.GUID)
			if err != nil {
				log.Println("Ошибка вставки новости:", err)
				continue
			}

			// Добавление в список новых новостей
			newNews = append(newNews, News{
				Title:       item.Title,
				Description: item.Description,
				Date:        formattedDate,
				Link:        item.Link,
				GUID:        item.GUID,
			})
		}
	}

	// Получение всех новостей из базы для отправки клиентам
	allNews, err := fetchAllNews(db)
	if err != nil {
		return nil, err
	}

	return allNews, nil
}

// Функция для получения всех новостей из базы данных
func fetchAllNews(db *sql.DB) ([]News, error) {
	rows, err := db.Query("SELECT id, title, description, date, link, guid FROM iu9Trofimenko ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newsList []News
	for rows.Next() {
		var n News
		err := rows.Scan(&n.ID, &n.Title, &n.Description, &n.Date, &n.Link, &n.GUID)
		if err != nil {
			log.Println("Ошибка сканирования строки:", err)
			continue
		}
		newsList = append(newsList, n)
	}
	return newsList, nil
}

// Обработчик WebSocket соединений
func handleConnections(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка обновления WebSocket:", err)
		return
	}
	defer ws.Close()

	// Регистрируем клиента
	clientsMutex.Lock()
	clients[ws] = true
	clientsMutex.Unlock()

	// Отправка текущих новостей при подключении
	allNews, err := fetchAllNews(db)
	if err != nil {
		log.Println("Ошибка получения новостей для отправки клиенту:", err)
	} else {
		err = ws.WriteJSON(allNews)
		if err != nil {
			log.Println("Ошибка отправки новостей клиенту:", err)
		}
	}

	// Прослушивание закрытия соединения
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			// Удаляем клиента при ошибке
			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()
			break
		}
	}
}

// Функция для отправки данных всем подключенным клиентам
func handleMessages() {
	for {
		news := <-broadcast

		clientsMutex.Lock()
		for client := range clients {
			err := client.WriteJSON(news)
			if err != nil {
				log.Printf("Ошибка отправки сообщения клиенту: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
		clientsMutex.Unlock()
	}
}

// Функция для периодического обновления RSS и базы данных
func periodicUpdate(db *sql.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		allNews, err := parseAndUpdate(db)
		if err != nil {
			log.Println("Ошибка при парсинге и обновлении:", err)
		} else {
			// Отправка обновленных новостей через канал broadcast
			broadcast <- allNews
			log.Println("Обновление базы данных и отправка новостей клиентам.")
		}

		<-ticker.C
	}
}

func main() {
	// Установка соединения с базой данных
	db, err := setupDatabase()
	if err != nil {
		log.Fatalf("Не удалось подключиться к базе данных: %v", err)
	}
	defer db.Close()

	// Первичное парсинг RSS и обновление базы данных
	allNews, err := parseAndUpdate(db)
	if err != nil {
		log.Fatalf("Не удалось выполнить первичное обновление: %v", err)
	}

	// Запуск горутины для отправки сообщений клиентам
	go handleMessages()

	// Запуск горутины для периодического обновления
	go periodicUpdate(db, 10*time.Minute) // Обновление каждые 10 минут

	// Маршруты
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleConnections(w, r, db)
	})

	// Обслуживание dashboard.html
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "dashboard.html")
	})

	// Первоначальная отправка новостей
	go func() {
		broadcast <- allNews
	}()

	// Запуск HTTP сервера на порту 9742
	addr := ":9742"
	log.Printf("Сервер запущен на http://localhost%s", addr)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}
