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
	fmt.Printf("The server is running on http://%s\n", addr)
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
			http.Error(w, "Failed to connect to FTP server: "+err.Error(), http.StatusInternalServerError)
			return
		}
		err = c.Login(ftpUser, ftpPass)
		if err != nil {
			http.Error(w, "Failed to login: "+err.Error(), http.StatusUnauthorized)
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
		log.Println("Error upgrading to WebSocket:", err)
		return
	}
	defer ws.Close()
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		ws.WriteMessage(websocket.TextMessage, []byte("Session ID is missing"))
		return
	}
	session, ok := sessions[sessionID]
	if !ok {
		ws.WriteMessage(websocket.TextMessage, []byte("Invalid session ID"))
		return
	}
	defer session.Client.Quit()
	delete(sessions, sessionID)
	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			log.Println("Error reading message:", err)
			break
		}
		cmd := string(message)
		response := handleCommand(session.Client, cmd)
		err = ws.WriteMessage(websocket.TextMessage, []byte(response))
		if err != nil {
			log.Println("Error sending message:", err)
			break
		}
	}
}
func handleCommand(c *ftp.ServerConn, cmd string) string {
	args := parseCommand(cmd)
	if len(args) == 0 {
		return "Command not entered"
	}
	switch args[0] {
	case "ls":
		return listDir(c)
	case "cd":
		if len(args) < 2 {
			return "Usage: cd <directory>"
		}
		return changeDir(c, args[1])
	case "upload":
		return "Loading via web interface is not implemented"
	case "download":
		return "Downloading via web interface is not implemented"
	case "mkdir":
		if len(args) < 2 {
			return "Usage: mkdir <directory_name>"
		}
		return makeDir(c, args[1])
	case "delete":
		if len(args) < 2 {
			return "Usage: delete <file_name>"
		}
		return deleteFile(c, args[1])
	case "rmdir":
		if len(args) < 2 {
			return "Usage: rmdir <directory>"
		}
		return removeDir(c, args[1], false)
	case "rmr":
		if len(args) < 2 {
			return "Usage: rmr <directory>"
		}
		err := removeDirRecursively(c, args[1])
		if err != nil {
			return "Error recursively deleting directory: " + err.Error()
		}
		return "Directory was deleted recursively"
	default:
		return "Unknown command. Available commands: upload, download, mkdir, delete, ls, cd, rmdir, rmr, quit"
	}
}
func parseCommand(input string) []string {
	input = strings.TrimSpace(input)
	return strings.Fields(input)
}
func listDir(c *ftp.ServerConn) string {
	entries, err := c.List("")
	if err != nil {
		return "Error getting directory listing: " + err.Error()
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
		return "Error changing directory: " + err.Error()
	}
	return "Current directory was changed"
}
func makeDir(c *ftp.ServerConn, dirName string) string {
	err := c.MakeDir(dirName)
	if err != nil {
		return "Error creating directory: " + err.Error()
	}
	return "Directory was created"
}
func deleteFile(c *ftp.ServerConn, fileName string) string {
	err := c.Delete(fileName)
	if err != nil {
		return "Error deleting file: " + err.Error()
	}
	return "File was deleted "
}
func removeDir(c *ftp.ServerConn, dir string, recursive bool) string {
	if recursive {
		err := removeDirRecursively(c, dir)
		if err != nil {
			return "Error recursively deleting directory: " + err.Error()
		}
		return "Directory was deleted recursively"
	} else {
		err := c.RemoveDir(dir)
		if err != nil {
			return "Error deleting directory: " + err.Error()
		}
		return "Directory was deleted"
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
