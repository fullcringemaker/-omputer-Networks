package main

import (
	"fmt"

	"github.com/SlyMarbo/rss"
)

func main() {

	rssObject, err := rss.Fetch("https://news.rambler.ru/rss/Magadan/")
	if err != nil {
		fmt.Println(err)
		panic("failed")
	}

	fmt.Printf("Title           : %s\n", rssObject.Title)
	// fmt.Printf("Generator       : %s\n", rssObject.Generator)
	// fmt.Printf("PubDate         : %s\n", rssObject.PubDate)
	// fmt.Printf("LastBuildDate   : %s\n", rssObject.LastBuildDate)
	fmt.Printf("Description     : %s\n", rssObject.Description)
	fmt.Printf("Number of Items : %d\n", len(rssObject.Items))

	for v := range rssObject.Items {
		item := rssObject.Items[v]
		fmt.Println()
		fmt.Printf("Item Number : %d\n", v)
		fmt.Printf("Title       : %s\n", item.Title)
		fmt.Printf("Link        : %s\n", item.Link)
		// fmt.Printf("Description : %s\n", item.Description)
		// fmt.Printf("Guid        : %s\n", item.Guid.Value)
	}
}
