package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

const (
	ftpHost = "students.yss.su:21" // Стандартный порт FTP
	ftpUser = "ftpiu8"
	ftpPass = "3Ru7yOTA"
)

func main() {
	// Подключение к FTP-серверу
	c, err := ftp.Dial(ftpHost, ftp.DialWithTimeout(5*time.Second))
	if err != nil {
		log.Fatalf("Не удалось подключиться к FTP-серверу: %v", err)
	}
	defer c.Quit()

	// Авторизация
	err = c.Login(ftpUser, ftpPass)
	if err != nil {
		log.Fatalf("Не удалось авторизоваться: %v", err)
	}
	fmt.Println("Успешно подключились и авторизовались на FTP-сервере.")

	// Интерактивный интерфейс команд
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("ftp> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		args := strings.Split(input, " ")
		cmd := strings.ToLower(args[0])

		switch cmd {
		case "upload":
			if len(args) != 2 {
				fmt.Println("Использование: upload <local_path>")
				continue
			}
			uploadFile(c, args[1])
		case "download":
			if len(args) != 2 {
				fmt.Println("Использование: download <remote_path>")
				continue
			}
			downloadFile(c, args[1])
		case "mkdir":
			if len(args) != 2 {
				fmt.Println("Использование: mkdir <directory_name>")
				continue
			}
			makeDir(c, args[1])
		case "delete":
			if len(args) != 2 {
				fmt.Println("Использование: delete <remote_file>")
				continue
			}
			deleteFile(c, args[1])
		case "ls":
			listDir(c)
		case "cd":
			if len(args) != 2 {
				fmt.Println("Использование: cd <directory>")
				continue
			}
			changeDir(c, args[1])
		case "rmdir":
			if len(args) != 2 {
				fmt.Println("Использование: rmdir <directory>")
				continue
			}
			removeDir(c, args[1], false)
		case "rmr":
			if len(args) != 2 {
				fmt.Println("Использование: rmr <directory>")
				continue
			}
			removeDir(c, args[1], true)
		case "quit", "exit":
			fmt.Println("Выход из FTP-клиента.")
			return
		default:
			fmt.Println("Неизвестная команда. Доступные команды: upload, download, mkdir, delete, ls, cd, rmdir, rmr, quit")
		}
	}
}

func uploadFile(c *ftp.ServerConn, localPath string) {
	file, err := os.Open(localPath)
	if err != nil {
		fmt.Printf("Ошибка открытия локального файла: %v\n", err)
		return
	}
	defer file.Close()

	remotePath := filepath.Base(localPath)
	err = c.Stor(remotePath, file)
	if err != nil {
		fmt.Printf("Ошибка загрузки файла: %v\n", err)
		return
	}
	fmt.Println("Файл успешно загружен.")
}

func downloadFile(c *ftp.ServerConn, remotePath string) {
	r, err := c.Retr(remotePath)
	if err != nil {
		fmt.Printf("Ошибка скачивания файла: %v\n", err)
		return
	}
	defer r.Close()

	localPath := filepath.Base(remotePath)
	file, err := os.Create(localPath)
	if err != nil {
		fmt.Printf("Ошибка создания локального файла: %v\n", err)
		return
	}
	defer file.Close()

	_, err = io.Copy(file, r)
	if err != nil {
		fmt.Printf("Ошибка записи в локальный файл: %v\n", err)
		return
	}
	fmt.Println("Файл успешно скачан.")
}

func makeDir(c *ftp.ServerConn, dirName string) {
	err := c.MakeDir(dirName)
	if err != nil {
		fmt.Printf("Ошибка создания директории: %v\n", err)
		return
	}
	fmt.Println("Директория успешно создана.")
}

func deleteFile(c *ftp.ServerConn, fileName string) {
	err := c.Delete(fileName)
	if err != nil {
		fmt.Printf("Ошибка удаления файла: %v\n", err)
		return
	}
	fmt.Println("Файл успешно удален.")
}

func listDir(c *ftp.ServerConn) {
	entries, err := c.List("")
	if err != nil {
		fmt.Printf("Ошибка получения списка директории: %v\n", err)
		return
	}
	for _, entry := range entries {
		fmt.Printf("%s\t%s\t%d\n", entry.Type, entry.Name, entry.Size)
	}
}

func changeDir(c *ftp.ServerConn, dir string) {
	err := c.ChangeDir(dir)
	if err != nil {
		fmt.Printf("Ошибка смены директории: %v\n", err)
		return
	}
	fmt.Println("Текущая директория изменена.")
}

func removeDir(c *ftp.ServerConn, dir string, recursive bool) {
	if recursive {
		// Рекурсивное удаление директории
		err := removeDirRecursively(c, dir)
		if err != nil {
			fmt.Printf("Ошибка рекурсивного удаления директории: %v\n", err)
			return
		}
		fmt.Println("Директория успешно рекурсивно удалена.")
	} else {
		// Удаление пустой директории
		err := c.RemoveDir(dir)
		if err != nil {
			fmt.Printf("Ошибка удаления директории: %v\n", err)
			return
		}
		fmt.Println("Директория успешно удалена.")
	}
}

func removeDirRecursively(c *ftp.ServerConn, dir string) error {
	entries, err := c.List(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Type == ftp.EntryTypeFolder {
			err := removeDirRecursively(c, filepath.Join(dir, entry.Name))
			if err != nil {
				return err
			}
			err = c.RemoveDir(filepath.Join(dir, entry.Name))
			if err != nil {
				return err
			}
		} else {
			err := c.Delete(filepath.Join(dir, entry.Name))
			if err != nil {
				return err
			}
		}
	}
	return c.RemoveDir(dir)
}
