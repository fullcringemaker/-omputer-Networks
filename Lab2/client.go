package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {

	url := "http://localhost:9742/"
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal("Error making request:", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatal("Error: server returned", resp.Status)
	}

	outFile, err := os.Create("output.html")
	if err != nil {
		log.Fatal("Error creating file:", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		log.Fatal("Error writing response to file:", err)
	}

	fmt.Println("HTML page saved as output.html")
}
