package main

import (
    "fmt"
    "log"
    "os"

    "github.com/goftp/server"
)

const (
    ftpUsername = "user"      // Простой логин
    ftpPassword = "password"  // Простой пароль
    ftpPort     = 9742        // Порт сервера
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

    // Настройка конфигурации FTP-сервера
    conf := &server.Config{
        Factory: &server.SimpleDriverFactory{
            RootPath: ftpRoot,
        },
        Port: ftpPort,
        Auth: &server.SimpleAuth{
            Credentials: map[string]string{
                ftpUsername: ftpPassword,
            },
        },
        // Параметры разрешений (чтение и запись)
        Perm: server.NewSimplePerm("read", "write"),
        // Опционально: Настройка пассивных портов
        // PassivePorts: []int{3000, 3001, 3002},
    }

    // Создание нового FTP-сервера с заданной конфигурацией
    s := server.NewServer(conf)

    fmt.Printf("Запуск FTP-сервера на порту %d...\n", ftpPort)
    if err := s.ListenAndServe(); err != nil {
        log.Fatalf("Ошибка запуска FTP-сервера: %v", err)
    }
}
