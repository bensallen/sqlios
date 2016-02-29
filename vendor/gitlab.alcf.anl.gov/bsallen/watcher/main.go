package main

import (
	"flag"
	"log"

	"github.com/fsnotify/fsnotify"
	"gitlab.alcf.anl.gov/bsallen/watcher/watch"
)

var path = flag.String("path", "", "directory or file path to watch")
var filter = flag.String("filter", "", "Regex filter for paths displayed")

func main() {

	flag.Parse()
	var errc = make(chan error, 10)
	var eventc = make(chan *fsnotify.Event)

	// Startup an err channel handler
	go func() {
		for err := range errc {
			log.Fatalf("Error, %s", err)
		}
	}()

	go func() {
		for event := range eventc {
			log.Println("event:", event)
		}
	}()

	done := make(chan bool)

	watch.Watch(*path, *filter, eventc, errc)

	<-done
}
