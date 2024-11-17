package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

const (
	ftpHost = "students.yss.su:21"
	ftpUser = "ftpiu8"
	ftpPass = "3Ru7yOTA"
)

func main() {
	c, err := ftp.Dial(ftpHost, ftp.DialWithTimeout(5*time.Second))
	if err != nil {
		log.Fatalf("Failed to connect to FTP server: %v", err)
	}
	defer c.Quit()
	err = c.Login(ftpUser, ftpPass)
	if err != nil {
		log.Fatalf("Failed to login: %v", err)
	}
	fmt.Println("Connection and authorization to the FTP server completed")
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
				fmt.Println("Usage: upload <local_path>")
				continue
			}
			uploadFile(c, args[1])
		case "download":
			if len(args) != 2 {
				fmt.Println("Usage: download <remote_path>")
				continue
			}
			downloadFile(c, args[1])
		case "mkdir":
			if len(args) != 2 {
				fmt.Println("Usage: mkdir <directory_name>")
				continue
			}
			makeDir(c, args[1])
		case "delete":
			if len(args) != 2 {
				fmt.Println("Usage: delete <remote_file>")
				continue
			}
			deleteFile(c, args[1])
		case "ls":
			listDir(c)
		case "cd":
			if len(args) != 2 {
				fmt.Println("Usage: cd <directory>")
				continue
			}
			changeDir(c, args[1])
		case "rmdir":
			if len(args) != 2 {
				fmt.Println("Usage: rmdir <directory>")
				continue
			}
			removeDir(c, args[1], false)
		case "rmr":
			if len(args) != 2 {
				fmt.Println("Usage: rmr <directory>")
				continue
			}
			err := removeDirRecursively(c, args[1])
			if err != nil {
				fmt.Printf("Error recursively deleting directory: %v\n", err)
			} else {
				fmt.Println("Directory was deleted recursively")
			}
		case "quit", "exit":
			fmt.Println("Exit FTP client")
			return
		default:
			fmt.Println("Unknown command. Available commands: upload, download, mkdir, delete, ls, cd, rmdir, rmr, quit")
		}
	}
}
func uploadFile(c *ftp.ServerConn, localPath string) {
	file, err := os.Open(localPath)
	if err != nil {
		fmt.Printf("Error opening local file: %v\n", err)
		return
	}
	defer file.Close()
	remotePath := path.Base(localPath)
	err = c.Stor(remotePath, file)
	if err != nil {
		fmt.Printf("File download error: %v\n", err)
		return
	}
	fmt.Println("File was uploaded")
}
func downloadFile(c *ftp.ServerConn, remotePath string) {
	r, err := c.Retr(remotePath)
	if err != nil {
		fmt.Printf("Error downloading file: %v\n", err)
		return
	}
	defer r.Close()
	localPath := path.Base(remotePath)
	file, err := os.Create(localPath)
	if err != nil {
		fmt.Printf("Error creating local file: %v\n", err)
		return
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	if err != nil {
		fmt.Printf("Error writing to local file: %v\n", err)
		return
	}
	fmt.Println("File was downloaded")
}
func makeDir(c *ftp.ServerConn, dirName string) {
	err := c.MakeDir(dirName)
	if err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}
	fmt.Println("Directory was created")
}
func deleteFile(c *ftp.ServerConn, fileName string) {
	err := c.Delete(fileName)
	if err != nil {
		fmt.Printf("Error deleting file: %v\n", err)
		return
	}
	fmt.Println("File was deleted")
}
func listDir(c *ftp.ServerConn) {
	entries, err := c.List("")
	if err != nil {
		fmt.Printf("Error getting directory listing: %v\n", err)
		return
	}
	for _, entry := range entries {
		fmt.Printf("%s\t%s\t%d\n", entry.Type, entry.Name, entry.Size)
	}
}
func changeDir(c *ftp.ServerConn, dir string) {
	err := c.ChangeDir(dir)
	if err != nil {
		fmt.Printf("Error changing directory: %v\n", err)
		return
	}
	fmt.Println("Current directory was changed")
}
func removeDir(c *ftp.ServerConn, dir string, recursive bool) {
	if recursive {
		err := removeDirRecursively(c, dir)
		if err != nil {
			fmt.Printf("Error recursively deleting directory: %v\n", err)
			return
		}
		fmt.Println("Directory was deleted recursively")
	} else {
		err := c.RemoveDir(dir)
		if err != nil {
			fmt.Printf("Error deleting directory: %v\n", err)
			return
		}
		fmt.Println("Directory was deleted")
	}
}
func removeDirRecursively(c *ftp.ServerConn, dir string) error {
	fmt.Printf("Start of recursive directory deletion: %s\n", dir)
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
			fmt.Printf("Directory was deleted: %s\n", fullPath)
		} else {
			err = c.Delete(fullPath)
			if err != nil {
				return err
			}
			fmt.Printf("File was deleted: %s\n", fullPath)
		}
	}
	err = c.RemoveDir(dir)
	if err != nil {
		return err
	}
	fmt.Printf("Directory %s was deleted recursively\n", dir)
	return nil
}
