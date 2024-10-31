// ssh_server.go
package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

func main() {
	server := ssh.Server{
		Addr: ":9742",
		Handler: func(s ssh.Session) {
			term := terminal.NewTerminal(s, "> ")
			for {
				// Чтение команды от пользователя
				line, err := term.ReadLine()
				if err != nil {
					if err == io.EOF {
						break
					}
					io.WriteString(s, "Ошибка чтения команды: "+err.Error()+"\n")
					continue
				}

				// Разбор команды
				args := strings.Fields(line)
				if len(args) == 0 {
					continue
				}

				switch args[0] {
				case "exit", "quit":
					io.WriteString(s, "Выход из сессии.\n")
					return
				case "mkdir":
					if len(args) < 2 {
						io.WriteString(s, "Использование: mkdir <путь>\n")
						continue
					}
					err := os.MkdirAll(args[1], 0755)
					if err != nil {
						io.WriteString(s, "Ошибка создания директории: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "Директория создана.\n")
					}
				case "rmdir":
					if len(args) < 2 {
						io.WriteString(s, "Использование: rmdir <путь>\n")
						continue
					}
					err := os.RemoveAll(args[1])
					if err != nil {
						io.WriteString(s, "Ошибка удаления директории: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "Директория удалена.\n")
					}
				case "ls":
					path := "."
					if len(args) > 1 {
						path = args[1]
					}
					files, err := os.ReadDir(path)
					if err != nil {
						io.WriteString(s, "Ошибка чтения директории: "+err.Error()+"\n")
						continue
					}
					for _, file := range files {
						io.WriteString(s, file.Name()+"\n")
					}
				case "mv":
					if len(args) < 3 {
						io.WriteString(s, "Использование: mv <источник> <назначение>\n")
						continue
					}
					err := os.Rename(args[1], args[2])
					if err != nil {
						io.WriteString(s, "Ошибка перемещения файла: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "Файл перемещен.\n")
					}
				case "rm":
					if len(args) < 2 {
						io.WriteString(s, "Использование: rm <имя файла>\n")
						continue
					}
					err := os.Remove(args[1])
					if err != nil {
						io.WriteString(s, "Ошибка удаления файла: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "Файл удален.\n")
					}
				case "ping":
					if len(args) < 2 {
						io.WriteString(s, "Использование: ping <адрес>\n")
						continue
					}
					cmd := exec.Command("ping", "-c", "4", args[1])
					output, err := cmd.CombinedOutput()
					if err != nil {
						io.WriteString(s, "Ошибка выполнения ping: "+err.Error()+"\n")
					}
					io.WriteString(s, string(output))
				case "touch":
					if len(args) < 2 {
						io.WriteString(s, "Использование: touch <путь к файлу>\n")
						continue
					}
					file, err := os.Create(args[1])
					if err != nil {
						io.WriteString(s, "Ошибка создания файла: "+err.Error()+"\n")
						continue
					}
					file.Close()
					io.WriteString(s, "Файл создан.\n")
				default:
					io.WriteString(s, "Неизвестная команда.\n")
				}
			}
		},
		PasswordHandler: func(ctx ssh.Context, password string) bool {
			log.Printf("Попытка авторизации: пользователь=%s пароль=%s\n", ctx.User(), password)
			return ctx.User() == "testuser" && password == "password123"
		},
	}

	log.Println("Запуск SSH-сервера на порту 9742...")
	log.Fatal(server.ListenAndServe())
}
