package main

import (
    "fmt"
    "log"
    "os"
    "path/filepath"

    "github.com/goftp/file-driver"
    "github.com/goftp/server"
)

// Простые учетные данные для авторизации
const (
    ftpUsername = "user"
    ftpPassword = "password"
    ftpPort     = "9742"
    ftpRoot     = "./ftproot" // Корневая директория FTP-сервера
)

func main() {
    // Проверка наличия корневой директории, если нет - создаем
    if _, err := os.Stat(ftpRoot); os.IsNotExist(err) {
        err := os.MkdirAll(ftpRoot, os.ModePerm)
        if err != nil {
            log.Fatalf("Не удалось создать корневую директорию: %v", err)
        }
    }

    // Настройка авторизатора
    auth := &Auth{
        Credentials: map[string]string{
            ftpUsername: ftpPassword,
        },
    }

    // Настройка файлового драйвера
    driver := filedriver.NewDriver(ftpRoot)

    // Настройка FTP-сервера
    s := server.NewServer(&server.Config{
        Factory:  driver,
        Port:     ftpPort,
        Auth:     auth,
        Perm:     server.NewSimplePerm("user", "user", "user"),
        PassivePorts: []int{3000, 3001, 3002}, // Порты для пассивных соединений
    })

    // Запуск FTP-сервера
    fmt.Printf("Запуск FTP-сервера на порту %s...\n", ftpPort)
    if err := s.ListenAndServe(); err != nil {
        log.Fatalf("Ошибка запуска FTP-сервера: %v", err)
    }
}

// Структура для авторизации
type Auth struct {
    Credentials map[string]string
}

// Реализация интерфейса AuthUser
func (a *Auth) CheckPasswd(user, pass string) bool {
    if pwd, ok := a.Credentials[user]; ok {
        return pwd == pass
    }
    return false
}

func (a *Auth) GetUser(name string) (server.User, bool) {
    if _, ok := a.Credentials[name]; ok {
        return &server.SimpleUser{
            Name:   name,
            Dir:    ftpRoot,
            Perm:   server.NewSimplePerm(name, name, name),
        }, true
    }
    return nil, false
}
