package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	ftpserver "github.com/goftp/server"
)

const (
	serverPort = 9742
	username   = "user1"    // Простое имя пользователя
	password   = "password1" // Простой пароль
	rootDir    = "./ftp_root" // Корневая директория FTP-сервера
)

func main() {
	// Убедимся, что корневая директория существует
	err := os.MkdirAll(rootDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Не удалось создать корневую директорию: %v", err)
	}

	// Настройка аутентификации
	user := &ftpserver.User{
		Name:   username,
		Password: password,
		HomeDir: rootDir,
	}

	// Создание конфигурации сервера
	config := ftpserver.Config{
		Factory:  &SimpleDriverFactory{User: user},
		Port:     serverPort,
		Auth:     &SimpleAuth{User: user},
		Hostname: "0.0.0.0",
	}

	// Создание FTP-сервера
	s := ftpserver.NewServer(&config)

	log.Printf("FTP-сервер запущен на порту %d", serverPort)

	// Запуск сервера
	err = s.ListenAndServe()
	if err != nil {
		log.Fatalf("Ошибка запуска FTP-сервера: %v", err)
	}
}

// SimpleAuth реализует интерфейс аутентификации
type SimpleAuth struct {
	User *ftpserver.User
}

func (a *SimpleAuth) CheckPasswd(user, pass string) (bool, error) {
	if user == a.User.Name && pass == a.User.Password {
		return true, nil
	}
	return false, nil
}

func (a *SimpleAuth) GetUser(name string) (ftpserver.User, bool) {
	if name == a.User.Name {
		return *a.User, true
	}
	return ftpserver.User{}, false
}

// SimpleDriverFactory реализует интерфейс фабрики драйверов
type SimpleDriverFactory struct {
	User *ftpserver.User
}

func (f *SimpleDriverFactory) NewDriver(user ftpserver.User) (ftpserver.Driver, error) {
	return &LocalDriver{
		rootPath: filepath.Clean(user.HomeDir),
	}, nil
}

// LocalDriver реализует интерфейс драйвера для локальной файловой системы
type LocalDriver struct {
	rootPath string
	currentPath string
}

func (d *LocalDriver) Auth(ctx *ftpserver.Context, user string) (bool, error) {
	return true, nil
}

func (d *LocalDriver) ChangeDir(dir string) error {
	newPath := filepath.Join(d.currentPath, dir)
	fullPath := filepath.Join(d.rootPath, newPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ftpserver.ErrNotDir
	}
	d.currentPath = newPath
	return nil
}

func (d *LocalDriver) Getwd() (string, error) {
	return d.currentPath, nil
}

func (d *LocalDriver) ListDir(path string, callback func(ftpserver.Entry) error) error {
	fullPath := filepath.Join(d.rootPath, d.currentPath, path)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		ftpEntry := ftpserver.Entry{
			Name: entry.Name(),
		}
		if entry.IsDir() {
			ftpEntry.Type = ftpserver.EntryTypeFolder
		} else {
			ftpEntry.Type = ftpserver.EntryTypeFile
			ftpEntry.Size = info.Size()
		}
		err = callback(ftpEntry)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *LocalDriver) DeleteFile(path string) error {
	fullPath := filepath.Join(d.rootPath, d.currentPath, path)
	return os.Remove(fullPath)
}

func (d *LocalDriver) DeleteDir(path string) error {
	fullPath := filepath.Join(d.rootPath, d.currentPath, path)
	return os.Remove(fullPath)
}

func (d *LocalDriver) Rename(from, to string) error {
	fullFrom := filepath.Join(d.rootPath, d.currentPath, from)
	fullTo := filepath.Join(d.rootPath, d.currentPath, to)
	return os.Rename(fullFrom, fullTo)
}

func (d *LocalDriver) MakeDir(path string) error {
	fullPath := filepath.Join(d.rootPath, d.currentPath, path)
	return os.Mkdir(fullPath, os.ModePerm)
}

func (d *LocalDriver) GetFile(path string) (io.ReadCloser, error) {
	fullPath := filepath.Join(d.rootPath, d.currentPath, path)
	return os.Open(fullPath)
}

func (d *LocalDriver) StoreFile(path string, data io.Reader) error {
	fullPath := filepath.Join(d.rootPath, d.currentPath, path)
	file, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, data)
	return err
}

func (d *LocalDriver) StatFile(path string) (ftpserver.Entry, error) {
	fullPath := filepath.Join(d.rootPath, d.currentPath, path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return ftpserver.Entry{}, err
	}
	ftpEntry := ftpserver.Entry{
		Name: filepath.Base(fullPath),
	}
	if info.IsDir() {
		ftpEntry.Type = ftpserver.EntryTypeFolder
	} else {
		ftpEntry.Type = ftpserver.EntryTypeFile
		ftpEntry.Size = info.Size()
	}
	return ftpEntry, nil
}

func (d *LocalDriver) Close() error {
	return nil
}

