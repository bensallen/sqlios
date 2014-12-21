package main

import (
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/influxdb/influxdb/client"
	"gitlab.alcf.anl.gov/bsallen/influxios/influxios"
	"gopkg.in/alecthomas/kingpin.v1"
)

//Cmd line flags
var (
	input       = kingpin.Flag("input", "Input file").Required().Short('i').String()
	cpus        = kingpin.Flag("cpus", "Max number of CPUs to use").Short('c').Int()
	noop        = kingpin.Flag("noop", "Don't actually push any data to InfluxDB, just print the JSON output").Short('n').Bool()
	oneshot     = kingpin.Flag("oneshot", "Run once in the foreground and exit").Short('o').Bool()
	debug       = kingpin.Flag("debug", "Print debug output").Short('d').Bool()
	host        = kingpin.Flag("host", "InfluxDB host to connect to").Default("localhost:8086").Short('h').String()
	username    = kingpin.Flag("username", "InfluxDB user name to authenticate as").Default("root").Short('u').String()
	password    = kingpin.Flag("password", "Password to authenticate with").Default("root").Short('p').String()
	database    = kingpin.Flag("database", "InfluxDB database to connect to").Short('D').String()
	loadOnStart = kingpin.Flag("onstart", "Force input file to be loaded on start, do not wait for the file to be updated").Short('O').Bool()
)

func main() {
	var NCPU = runtime.NumCPU()
	runtime.GOMAXPROCS(NCPU)
	var numUploaders = NCPU * 2
	var numBlockParsers = NCPU * 2

	kingpin.Parse()

	if *cpus != 0 {
		runtime.GOMAXPROCS(*cpus)
		numUploaders = *cpus * 2
		numBlockParsers = *cpus * 2
	}

	file, err := os.Open(*input)

	if err != nil {
		log.Fatalf("os.Open: %s", err)
	}

	c, err := client.NewClient(&client.ClientConfig{
		Host:     *host,
		Username: *username,
		Password: *password,
		Database: *database,
	})

	if err != nil {
		log.Fatalf("client.NewClient: %s", err)
	}

	var filec = make(chan *os.File)
	var blockc = make(chan influxios.Block)
	var seriesc = make(chan *client.Series)
	var errc = make(chan error, 10)

	// Startup an err channel handler
	go func() {
		for err := range errc {
			log.Printf("Error, %s", err)
		}
	}()

	var wgReader sync.WaitGroup
	var wgUploaders sync.WaitGroup
	var wgBlockParsers sync.WaitGroup

	// Startup a single Reader
	wgReader.Add(1)
	go func() {
		influxios.Reader(blockc, filec, errc)
		wgReader.Done()
	}()

	// Startup the Uploaders
	wgUploaders.Add(numUploaders)
	for i := 0; i < numUploaders; i++ {
		go func() {
			influxios.Uploader(noop, c, seriesc, errc)
			wgUploaders.Done()
		}()
	}

	// Startup the Block parsers
	wgBlockParsers.Add(numBlockParsers)
	for i := 0; i < numBlockParsers; i++ {
		go func() {
			influxios.ParseBlock(blockc, seriesc, errc)
			wgBlockParsers.Done()
		}()
	}

	if *loadOnStart || *oneshot {
		filec <- file
	}

	if *oneshot == false {

		influxios.Watcher(input, filec, errc)

		done := make(chan bool)

		//Wait forever
		<-done
	}

	//The following needs to be in this very specfic order of closes and waits to
	//avoid panics from emitting or reading from a closed channel

	//Close filec so Reader will exit
	close(filec)
	wgReader.Wait()

	//Close blockc so influxios.ParseBlock will exit
	close(blockc)
	wgBlockParsers.Wait()

	//Close seriesc so influxios.Uploader will exit
	close(seriesc)
	wgUploaders.Wait()

	//Finally close errc so the error handler exits
	close(errc)

}
