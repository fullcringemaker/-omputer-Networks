package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

const (
	ftpHost     = "students.yss.su:21"
	ftpUsername = "ftpiu8"
	ftpPassword = "3Ru7yOTA"
)

var currentDir = "."

// Подключение к FTP-серверу
func connect() (*ftp.ServerConn, error) {
	conn, err := ftp.Dial(ftpHost, ftp.DialWithTimeout(5*time.Second))
	if err != nil {
		return nil, err
	}

	err = conn.Login(ftpUsername, ftpPassword)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Функции для работы с FTP

func uploadFile(conn *ftp.ServerConn, localPath, remotePath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	err = conn.Stor(path.Join(currentDir, remotePath), file)
	if err != nil {
		return err
	}
	fmt.Println("Файл загружен:", remotePath)
	return nil
}

func downloadFile(conn *ftp.ServerConn, remotePath, localPath string) error {
	resp, err := conn.Retr(path.Join(currentDir, remotePath))
	if err != nil {
		return err
	}
	defer resp.Close()

	file, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.ReadFrom(resp)
	if err != nil {
		return err
	}
	fmt.Println("Файл скачан:", localPath)
	return nil
}

func createDirectory(conn *ftp.ServerConn, dirName string) error {
	err := conn.MakeDir(path.Join(currentDir, dirName))
	if err != nil {
		return err
	}
	fmt.Println("Директория создана:", dirName)
	return nil
}

func deleteFile(conn *ftp.ServerConn, filePath string) error {
	err := conn.Delete(path.Join(currentDir, filePath))
	if err != nil {
		return err
	}
	fmt.Println("Файл удален:", filePath)
	return nil
}

func listDirectory(conn *ftp.ServerConn, dirPath string) error {
	entries, err := conn.List(path.Join(currentDir, dirPath))
	if err != nil {
		return err
	}
	fmt.Println("Содержимое директории:", dirPath)
	for _, entry := range entries {
		fmt.Println(entry.Name)
	}
	return nil
}

func deleteDirectoryRecursive(c *ftp.ServerConn, dirPath string) error {
	fmt.Printf("Начинаю рекурсивное удаление директории: %s\n", dirPath)
	entries, err := c.List(path.Join(currentDir, dirPath))
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}

		fullPath := path.Join(currentDir, dirPath, entry.Name)
		if entry.Type == ftp.EntryTypeFolder {
			err = deleteDirectoryRecursive(c, fullPath)
			if err != nil {
				return err
			}
		} else {
			err = c.Delete(fullPath)
			if err != nil {
				return err
			}
			fmt.Printf("Файл %s удален\n", fullPath)
		}
	}

	err = c.RemoveDir(path.Join(currentDir, dirPath))
	if err != nil {
		return err
	}
	fmt.Printf("Директория %s успешно удалена рекурсивно\n", dirPath)
	return nil
}

// Функция для изменения директории
func changeDirectory(conn *ftp.ServerConn, dir string) error {
	err := conn.ChangeDir(dir)
	if err != nil {
		return err
	}
	currentDir = dir
	fmt.Println("Текущая директория изменена на:", currentDir)
	return nil
}

func main() {
	// Подключение к FTP-серверу
	conn, err := connect()
	if err != nil {
		log.Fatal("Ошибка подключения:", err)
	}
	defer conn.Quit()

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("FTP-клиент подключен.")

	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Ошибка чтения ввода: %s", err)
		}
		input = strings.TrimSpace(input)
		if input == "exit" {
			break
		}
		if input == "" {
			continue
		}

		args := strings.Split(input, " ")
		command := args[0]

		// Обработка команд
		switch command {
		case "upload":
			if len(args) < 3 {
				fmt.Println("Использование: upload <local_path> <remote_path>")
				continue
			}
			err := uploadFile(conn, args[1], args[2])
			if err != nil {
				fmt.Printf("Ошибка загрузки файла: %s\n", err)
			}

		case "download":
			if len(args) < 3 {
				fmt.Println("Использование: download <remote_path> <local_path>")
				continue
			}
			err := downloadFile(conn, args[1], args[2])
			if err != nil {
				fmt.Printf("Ошибка скачивания файла: %s\n", err)
			}

		case "mkdir":
			if len(args) < 2 {
				fmt.Println("Использование: mkdir <directory>")
				continue
			}
			err := createDirectory(conn, args[1])
			if err != nil {
				fmt.Printf("Ошибка создания директории: %s\n", err)
			}

		case "rm":
			if len(args) < 2 {
				fmt.Println("Использование: rm <file>")
				continue
			}
			err := deleteFile(conn, args[1])
			if err != nil {
				fmt.Printf("Ошибка удаления файла: %s\n", err)
			}

		case "ls":
			if len(args) < 2 {
				fmt.Println("Использование: ls <directory>")
				continue
			}
			err := listDirectory(conn, args[1])
			if err != nil {
				fmt.Printf("Ошибка получения содержимого директории: %s\n", err)
			}

		case "rmdir_recursive":
			if len(args) < 2 {
				fmt.Println("Использование: rmdir_recursive <directory>")
				continue
			}
			err := deleteDirectoryRecursive(conn, args[1])
			if err != nil {
				fmt.Printf("Ошибка рекурсивного удаления директории: %s\n", err)
			}

		case "cd":
			if len(args) < 2 {
				fmt.Println("Использование: cd <directory>")
				continue
			}
			err := changeDirectory(conn, args[1])
			if err != nil {
				fmt.Printf("Ошибка смены директории: %s\n", err)
			}

		default:
			fmt.Println("Неизвестная команда:", command)
		}
	}
}
