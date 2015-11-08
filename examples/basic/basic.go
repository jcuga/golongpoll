package main

import (
    "log"

    "github.com/jcuga/golongpoll"
)

// TODO: Comments about usage
// TODO: index.html
func main() {
    manager, err := golongpoll.CreateManager()
    if err != nil {
        log.Fatalf("Failed to create manager: %q", err)
    }

    manager.Publish("animals", "cow")

    manager.Shutdown()

    // TODO: basic web hook
    // TODO: exclude from test coverage
}
