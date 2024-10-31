// ssh_server.go
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gliderlabs/ssh"
)

func main() {
	// Настройка SSH-сервера
	server := ssh.Server{
		Addr: ":9742",
		Handler: func(s ssh.Session) {
			// Получение команды от клиента
			cmd := s.RawCommand()
			if cmd == "" {
				io.WriteString(s, "Пожалуйста, введите команду.\n")
				return
			}

			// Разбор команды
			args := strings.Fields(cmd)
			switch args[0] {
			case "mkdir":
				if len(args) < 2 {
					io.WriteString(s, "Использование: mkdir <путь>\n")
					return
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
					return
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
					return
				}
				for _, file := range files {
					io.WriteString(s, file.Name()+"\n")
				}
			case "mv":
				if len(args) < 3 {
					io.WriteString(s, "Использование: mv <источник> <назначение>\n")
					return
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
					return
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
					return
				}
				out, err := exec.Command("ping", "-c", "4", args[1]).Output()
				if err != nil {
					io.WriteString(s, "Ошибка выполнения ping: "+err.Error()+"\n")
				} else {
					io.WriteString(s, string(out))
				}
			default:
				io.WriteString(s, "Неизвестная команда.\n")
			}
		},
		PasswordHandler: func(ctx ssh.Context, password string) bool {
			// Простая проверка пароля
			return password == "password123"
		},
	}

	log.Println("Запуск SSH-сервера на порту 9742...")
	log.Fatal(server.ListenAndServe())
}

