package main

import (
	"flag"
	"fmt"
	"net/url"
	"time"

	"github.com/jcuga/golongpoll/client"
)

func main() {
	publishUrl := flag.String("publishUrl", "http://127.0.0.1:8080/publish", "URL to server's publish hook.")
	category := flag.String("category", "testing", "Longpoll category to publish to.")
	delayMs := flag.Uint("delayMs", 100, "Delay in milliseconds between event publish calls.")
	count := flag.Uint("count", 1000, "Number of events to publish before stopping.")
	flag.Parse()

	u, err := url.Parse(*publishUrl)
	if err != nil {
		panic(err)
	}

	lpClient, err := client.NewClient(client.ClientOptions{
		PublishUrl:     *u,
		Category:       *category,
		LoggingEnabled: true,
	})
	if err != nil {
		fmt.Println("FAILED TO CREATE LONGPOLL CLIENT: ", err)
		return
	}

	fmt.Printf("Publishing %d events with %d ms delay to %s on category: %s\n", *count, *delayMs, *publishUrl, *category)
	for i := uint(0); i < *count; i++ {
		pubErr := lpClient.Publish(*category, fmt.Sprintf("Event #%d", i))
		if pubErr != nil {
			fmt.Printf("Error publishing event, error: %v", pubErr)
		}
		time.Sleep(time.Duration(*delayMs) * time.Millisecond)
	}
	fmt.Println("Done.")
}
