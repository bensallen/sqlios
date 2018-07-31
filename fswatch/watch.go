package watch

import (
	"log"
	"os"
	"regexp"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch watches a given path for file system events. It can filter the events it returns based
// on a regex filter.
func Watch(path string, filter string, eventc chan *fsnotify.Event, errc chan error) {

	fileInfo, err := os.Stat(path)
	if err != nil {
		errc <- err
		return
	}
	isDir := fileInfo.IsDir()

	re, err := regexp.Compile(filter)
	if err != nil {
		errc <- err
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		errc <- err
		return
	}

	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if filter != "" && re.MatchString(event.Name) {
					//log.Println("event:", event)
					eventc <- &event
				} else if filter == "" {
					//log.Println("event:", event)
					eventc <- &event
				}
				if event.Op&fsnotify.Remove == fsnotify.Remove && isDir == false {
					for {
						err = watcher.Add(path)
						if err == nil {
							break
						}
						log.Print(err)
						time.Sleep(1 * time.Second)
					}
				}
			case err := <-watcher.Errors:
				//errc <- err
				log.Print(err)
			}
		}
	}()

	err = watcher.Add(path)
	if err != nil {
		errc <- err
		return
	}
	return
}
