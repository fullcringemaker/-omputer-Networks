package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jlaffaye/ftp"
)

type FtpSession struct {
	Client *ftp.ServerConn
}

var (
	upgrader   = websocket.Upgrader{}
	sessions   = make(map[string]*FtpSession)
	templates  = template.Must(template.ParseFiles("index.html", "work.html"))
	serverIP   = "185.104.251.226"
	serverPort = 9742
)

func main() {
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/work", workHandler)
	http.HandleFunc("/ws", wsHandler)

	addr := fmt.Sprintf("%s:%d", serverIP, serverPort)
	fmt.Printf("Сервер запущен на http://%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		templates.ExecuteTemplate(w, "index.html", nil)
	} else if r.Method == http.MethodPost {
		ftpHost := r.FormValue("ftpHost")
		ftpUser := r.FormValue("ftpUser")
		ftpPass := r.FormValue("ftpPass")

		if !strings.Contains(ftpHost, ":") {
			ftpHost = ftpHost + ":21"
		}

		c, err := ftp.Dial(ftpHost, ftp.DialWithTimeout(5*time.Second))
		if err != nil {
			http.Error(w, "Не удалось подключиться к FTP-серверу: "+err.Error(), http.StatusInternalServerError)
			return
		}
		err = c.Login(ftpUser, ftpPass)
		if err != nil {
			http.Error(w, "Не удалось авторизоваться: "+err.Error(), http.StatusUnauthorized)
			return
		}

		sessionID := fmt.Sprintf("%s-%d", strings.ReplaceAll(r.RemoteAddr, ":", "-"), time.Now().UnixNano())

		sessions[sessionID] = &FtpSession{Client: c}

		http.Redirect(w, r, "/work?session="+sessionID, http.StatusFound)
	}
}

func workHandler(w http.ResponseWriter, r *http.Request) {
	templates.ExecuteTemplate(w, "work.html", nil)
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка обновления до WebSocket:", err)
		return
	}
	defer ws.Close()

	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		ws.WriteMessage(websocket.TextMessage, []byte("Идентификатор сессии отсутствует"))
		return
	}

	// Получаем FTP-сессию
	session, ok := sessions[sessionID]
	if !ok {
		ws.WriteMessage(websocket.TextMessage, []byte("Неверный идентификатор сессии"))
		return
	}
	defer session.Client.Quit()
	delete(sessions, sessionID)

	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			log.Println("Ошибка чтения сообщения:", err)
			break
		}
		cmd := string(message)
		response := handleCommand(session.Client, cmd)
		err = ws.WriteMessage(websocket.TextMessage, []byte(response))
		if err != nil {
			log.Println("Ошибка отправки сообщения:", err)
			break
		}
	}
}
func handleCommand(c *ftp.ServerConn, cmd string) string {
	args := parseCommand(cmd)
	if len(args) == 0 {
		return "Команда не введена"
	}

	switch args[0] {
	case "ls":
		return listDir(c)
	case "cd":
		if len(args) < 2 {
			return "Использование: cd <directory>"
		}
		return changeDir(c, args[1])
	case "upload":
		return "Загрузка через веб-интерфейс не реализована"
	case "download":
		return "Скачивание через веб-интерфейс не реализовано"
	case "mkdir":
		if len(args) < 2 {
			return "Использование: mkdir <directory_name>"
		}
		return makeDir(c, args[1])
	case "delete":
		if len(args) < 2 {
			return "Использование: delete <file_name>"
		}
		return deleteFile(c, args[1])
	case "rmdir":
		if len(args) < 2 {
			return "Использование: rmdir <directory>"
		}
		return removeDir(c, args[1], false)
	case "rmr":
		if len(args) < 2 {
			return "Использование: rmr <directory>"
		}
		err := removeDirRecursively(c, args[1])
		if err != nil {
			return "Ошибка рекурсивного удаления директории: " + err.Error()
		}
		return "Директория была рекурсивно удалена"
	default:
		return "Неизвестная команда"
	}
}
func parseCommand(input string) []string {
	input = strings.TrimSpace(input)
	return strings.Fields(input)
}

func listDir(c *ftp.ServerConn) string {
	entries, err := c.List("")
	if err != nil {
		return "Ошибка получения списка директорий: " + err.Error()
	}
	var result strings.Builder
	for _, entry := range entries {
		var typeIndicator string
		if entry.Type == ftp.EntryTypeFolder {
			typeIndicator = "[D]"
		} else {
			typeIndicator = "[F]"
		}
		result.WriteString(fmt.Sprintf("%s\t%s\n", typeIndicator, entry.Name))
	}
	return result.String()
}

func changeDir(c *ftp.ServerConn, dir string) string {
	err := c.ChangeDir(dir)
	if err != nil {
		return "Ошибка смены директории: " + err.Error()
	}
	return "Текущая директория была изменена"
}

func makeDir(c *ftp.ServerConn, dirName string) string {
	err := c.MakeDir(dirName)
	if err != nil {
		return "Ошибка создания директории: " + err.Error()
	}
	return "Директория была создана"
}

func deleteFile(c *ftp.ServerConn, fileName string) string {
	err := c.Delete(fileName)
	if err != nil {
		return "Ошибка удаления файла: " + err.Error()
	}
	return "Файл был удалён"
}

func removeDir(c *ftp.ServerConn, dir string, recursive bool) string {
	if recursive {
		err := removeDirRecursively(c, dir)
		if err != nil {
			return "Ошибка рекурсивного удаления директории: " + err.Error()
		}
		return "Директория была рекурсивно удалена"
	} else {
		err := c.RemoveDir(dir)
		if err != nil {
			return "Ошибка удаления директории: " + err.Error()
		}
		return "Директория была удалена"
	}
}

func removeDirRecursively(c *ftp.ServerConn, dir string) error {
	entries, err := c.List(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}
		fullPath := path.Join(dir, entry.Name)
		if entry.Type == ftp.EntryTypeFolder {
			err = removeDirRecursively(c, fullPath)
			if err != nil {
				return err
			}
		} else {
			err = c.Delete(fullPath)
			if err != nil {
				return err
			}
		}
	}
	err = c.RemoveDir(dir)
	if err != nil {
		return err
	}
	return nil
}
