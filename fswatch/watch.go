package fswatch

import (
	"log"
	"os"
	"path"
	"regexp"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch watches a given path for file system events. It can filter the events it returns based
// on a regex filter.
func watch(path string, filter string, eventc chan *fsnotify.Event, errc chan error) {

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

// Watcher watches the input file using inotify, knotifyd, etc for CREATE events.
// Nagios does atomic updates of status.dat by writting out to a temporary file,
// removing status.dat and moving the temporary file to status.dat. The last
// event seen in this process is a CREATE, so we use that to kick off reading.
// Watcher waits on the done chan forever.
func Watcher(input *string, filec chan *os.File, done chan bool, errc chan error) {

	path := path.Dir(*input)

	dir, err := os.Stat(path)

	if err != nil || !dir.IsDir() {
		log.Fatalf("Parent directory of input: %s, could not stat or is not a directory, watcher bailing!", path)
	}

	var eventc = make(chan *fsnotify.Event, 128)

	go func() {
		for event := range eventc {
			// TODO: What does this notation actually mean?
			if event.Op&fsnotify.Create == fsnotify.Create {
				file, err := os.Open(*input)
				if err != nil {
					errc <- err
				}
				filec <- file
			}
		}
	}()

	watch(path, *input, eventc, errc)

	//Wait until someone tells us we're done
	<-done
}
