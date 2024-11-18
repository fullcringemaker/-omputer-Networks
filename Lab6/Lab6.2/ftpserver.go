package main

import (
    "fmt"
    "log"
    "os"

    "github.com/goftp/server"
)

// Константы для настройки FTP-сервера
const (
    ftpUsername = "user"        // Простое имя пользователя
    ftpPassword = "password"    // Простой пароль
    ftpPort     = 9742          // Порт FTP-сервера
    ftpRoot     = "./ftproot"    // Корневая директория FTP-сервера
)

func main() {
    // Проверка наличия корневой директории, если нет — создание
    if _, err := os.Stat(ftpRoot); os.IsNotExist(err) {
        err := os.MkdirAll(ftpRoot, os.ModePerm)
        if err != nil {
            log.Fatalf("Не удалось создать корневую директорию: %v", err)
        }
    }

    // Настройка авторизации
    auth := &server.SimpleAuth{
        Credentials: map[string]string{
            ftpUsername: ftpPassword,
        },
    }

    // Настройка фабрики драйвера
    factory := &server.SimpleDriverFactory{
        RootPath: ftpRoot,
    }

    // Настройка разрешений (чтение и запись)
    perm := server.NewSimplePerm("read", "write")

    // Конфигурация FTP-сервера
    conf := &server.Config{
        Factory:       factory,
        Port:          ftpPort,
        Auth:          auth,
        Perm:          perm,
        PassivePorts:  []int{3000, 3001, 3002}, // Порты для пассивных соединений
        WelcomeMessage: "Добро пожаловать на FTP-сервер!",
    }

    // Создание FTP-сервера с заданной конфигурацией
    s := server.NewServer(conf)

    // Запуск FTP-сервера
    fmt.Printf("Запуск FTP-сервера на порту %d...\n", ftpPort)
    if err := s.ListenAndServe(); err != nil {
        log.Fatalf("Ошибка запуска FTP-сервера: %v", err)
    }
}
