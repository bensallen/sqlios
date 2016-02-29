package main

import (
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/alecthomas/kingpin"
	"github.com/influxdata/influxdb/client/v2"
	"github.com/pkg/profile"
	"gitlab.alcf.anl.gov/bsallen/influxios/influxios"
)

//Cmd line flags
var (
	input       = kingpin.Flag("input", "Input file").Required().Short('i').String()
	cpus        = kingpin.Flag("cpus", "Max number of CPUs to use").Short('c').Int()
	noop        = kingpin.Flag("noop", "Don't actually push any data to InfluxDB, just print the JSON output").Short('n').Bool()
	oneshot     = kingpin.Flag("oneshot", "Run once in the foreground and exit").Short('o').Bool()
	verbose     = kingpin.Flag("verbose", "Output more verbose information").Short('v').Bool()
	debug       = kingpin.Flag("debug", "Print debug output").Short('d').Bool()
	host        = kingpin.Flag("host", "InfluxDB host to connect to").Default("http://localhost:8086").Short('h').String()
	username    = kingpin.Flag("username", "InfluxDB user name to authenticate as").Default("root").Short('u').String()
	password    = kingpin.Flag("password", "Password to authenticate with").Default("root").Short('p').String()
	database    = kingpin.Flag("database", "InfluxDB database to connect to").Short('D').String()
	loadOnStart = kingpin.Flag("onstart", "Force input file to be loaded on start, do not wait for the file to be updated").Short('O').Bool()
	jsonOut     = kingpin.Flag("json", "Print out JSON output of data").Short('J').Bool()
	profileOut  = kingpin.Flag("profile", "Enable profile output").Short('P').String()
	startTime   = kingpin.Flag("last", "Specify epoch seconds as the start time, only entries after this time will be uploaded").Short('s').Int()
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

	if *profileOut != "" {
		switch *profileOut {
		case "cpu":
			defer profile.Start(profile.CPUProfile).Stop()
		case "mem":
			defer profile.Start(profile.MemProfile).Stop()
		case "block":
			defer profile.Start(profile.BlockProfile).Stop()
		default:
			// do nothing
		}
	}
	file, err := os.Open(*input)

	if err != nil {
		log.Fatalf("os.Open: %s", err)
	}

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     *host,
		Username: *username,
		Password: *password,
	})

	if err != nil {
		log.Fatalf("client.NewHTTPClient: %s", err)
	}

	var filec = make(chan *os.File, 10)
	var blockc = make(chan influxios.Block, 100)
	var pointc = make(chan *client.Point, 100)

	//Reader pushes a true to this channel at the end of each file. Uploader
	//consumes it to know when to print off the count of uploaded points
	var endOfFile = make(chan bool)

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
		influxios.Reader(blockc, filec, endOfFile, errc)
		wgReader.Done()
	}()

	// Startup the Uploaders
	wgUploaders.Add(numUploaders)
	for i := 0; i < numUploaders; i++ {
		go func() {
			influxios.Uploader(noop, jsonOut, c, pointc, endOfFile, errc)
			wgUploaders.Done()
		}()
	}

	// Startup the Block parsers
	wgBlockParsers.Add(numBlockParsers)
	for i := 0; i < numBlockParsers; i++ {
		go func() {
			influxios.ParseBlock(blockc, pointc, errc)
			wgBlockParsers.Done()
		}()
	}

	if *loadOnStart || *oneshot {
		filec <- file
	}

	if *oneshot == false {

		done := make(chan bool)

		influxios.Watcher(input, filec, done, errc)

	}

	//The following needs to be in this specfic order of closes and waits to
	//avoid panics from emitting or reading from a closed channel

	//Close filec so Reader will exit
	close(filec)
	wgReader.Wait()

	//Close blockc so influxios.ParseBlock will exit
	close(blockc)
	wgBlockParsers.Wait()

	//Close seriesc so influxios.Uploader will exit
	close(pointc)
	wgUploaders.Wait()

	//Finally close errc so the error handler exits
	close(errc)

}
