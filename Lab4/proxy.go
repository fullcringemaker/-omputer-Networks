package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

func main() {
	port := "9742"
	fs := http.FileServer(http.Dir("./assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", fs))
	http.HandleFunc("/", proxyHandler)

	fmt.Println("Proxy server running on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	targetPath := strings.TrimPrefix(r.URL.Path, "/")
	if targetPath == "" {
		http.Error(w, "The target URL is not specified.", http.StatusBadRequest)
		return
	}
	targetURLStr := ""
	if strings.HasPrefix(targetPath, "www.") || strings.HasPrefix(targetPath, "http") {
		targetURLStr = "https://" + targetPath
	} else {
		targetURLStr = "https://" + targetPath
	}
	targetURL, err := url.Parse(targetURLStr)
	if err != nil {
		http.Error(w, "Invalid target URL.", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequest(r.Method, targetURL.String(), nil)
	if err != nil {
		http.Error(w, "An error occurred while creating a request to the target URL.", http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	req.Header.Set("Accept-Encoding", "gzip")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to get target URL", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			http.Error(w, "Error unpacking gzip-response.", http.StatusInternalServerError)
			return
		}
		defer reader.(*gzip.Reader).Close()
	}
	resp.Header.Del("Content-Encoding")
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		bodyBytes, err := ioutil.ReadAll(reader)
		if err != nil {
			http.Error(w, "Failed to read response body.", http.StatusInternalServerError)
			return
		}
		modifiedBody, err := processHTML(bodyBytes, targetURL, r.Host)
		if err != nil {
			http.Error(w, "Failed to parse HTML.", http.StatusInternalServerError)
			return
		}
		w.Write(modifiedBody)
	} else if strings.Contains(contentType, "text/css") || strings.Contains(contentType, "application/javascript") {
		bodyBytes, err := ioutil.ReadAll(reader)
		if err != nil {
			http.Error(w, "Failed to read response body.", http.StatusInternalServerError)
			return
		}
		modifiedBody := processAssets(bodyBytes, targetURL, r.Host)
		w.Write(modifiedBody)
	} else {
		io.Copy(w, reader)
	}
}

func processHTML(body []byte, baseURL *url.URL, proxyHost string) ([]byte, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for i, attr := range n.Attr {
				if attr.Key == "href" || attr.Key == "src" || attr.Key == "action" {
					origLink := attr.Val
					if origLink == "" || strings.HasPrefix(origLink, "#") {
						continue
					}
					linkURL, err := baseURL.Parse(origLink)
					if err != nil || linkURL.Scheme == "data" || linkURL.Scheme == "mailto" || linkURL.Scheme == "javascript" {
						continue
					}
					if isAsset(linkURL.Path, n, attr.Key) {
						localPath, err := downloadAndSave(linkURL)
						if err == nil {
							n.Attr[i].Val = "/assets/" + localPath
						}
					} else {
						proxiedPath := "/" + linkURL.Host + linkURL.RequestURI()
						n.Attr[i].Val = "http://" + proxyHost + proxiedPath
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	var buf bytes.Buffer
	html.Render(&buf, doc)
	return buf.Bytes(), nil
}

func isAsset(path string, node *html.Node, attrKey string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp", ".woff", ".woff2", ".ttf", ".eot":
		return true
	case "":
		if node.Data == "link" && attrKey == "href" {
			for _, attr := range node.Attr {
				if attr.Key == "rel" && strings.Contains(attr.Val, "stylesheet") {
					return true
				}
			}
		}
		if node.Data == "script" && attrKey == "src" {
			return true
		}
		return false
	default:
		return false
	}
}

func downloadAndSave(linkURL *url.URL) (string, error) {
	req, err := http.NewRequest("GET", linkURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept-Encoding", "gzip")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return "", err
		}
		defer reader.(*gzip.Reader).Close()
	}
	bodyBytes, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/css") || strings.Contains(contentType, "javascript") {
		bodyBytes = processAssets(bodyBytes, linkURL, "")
	}
	assetPath := filepath.Join("assets", linkURL.Host, linkURL.Path)
	dirPath := filepath.Dir(assetPath)
	err = os.MkdirAll(dirPath, os.ModePerm)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(assetPath, bodyBytes, os.ModePerm)
	if err != nil {
		return "", err
	}
	localPath := filepath.Join(linkURL.Host, linkURL.Path)
	return localPath, nil
}

func processAssets(content []byte, baseURL *url.URL, proxyHost string) []byte {
	contentStr := string(content)
	reCSS := regexp.MustCompile(`url\(['"]?(.*?)['"]?\)`)
	contentStr = reCSS.ReplaceAllStringFunc(contentStr, func(match string) string {
		urlMatch := reCSS.FindStringSubmatch(match)
		if len(urlMatch) > 1 {
			origURL := urlMatch[1]
			if strings.HasPrefix(origURL, "data:") || strings.HasPrefix(origURL, "#") {
				return match
			}
			assetURL, err := baseURL.Parse(origURL)
			if err != nil {
				return match
			}
			localPath, err := downloadAndSave(assetURL)
			if err != nil {
				return match
			}
			newURL := "/assets/" + localPath
			return fmt.Sprintf(`url('%s')`, newURL)
		}
		return match
	})
	reJS := regexp.MustCompile(`(src|href)=['"]([^'"]+)['"]`)
	contentStr = reJS.ReplaceAllStringFunc(contentStr, func(match string) string {
		parts := reJS.FindStringSubmatch(match)
		if len(parts) > 2 {
			attr := parts[1]
			origURL := parts[2]
			if strings.HasPrefix(origURL, "data:") || strings.HasPrefix(origURL, "#") {
				return match
			}
			assetURL, err := baseURL.Parse(origURL)
			if err != nil {
				return match
			}
			localPath, err := downloadAndSave(assetURL)
			if err != nil {
				return match
			}
			newURL := "/assets/" + localPath
			return fmt.Sprintf(`%s='%s'`, attr, newURL)
		}
		return match
	})
	return []byte(contentStr)
}
