package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/influxdb/influxdb/client"
	"github.com/jprichardson/readline-go"
	"gopkg.in/alecthomas/kingpin.v1"
)

//Cmd line flags
var (
	input    = kingpin.Flag("input", "Input file").Required().Short('i').String()
	cpus     = kingpin.Flag("cpus", "Max number of CPUs to use").Short('c').Int()
	noop     = kingpin.Flag("noop", "Don't actually push any data to InfluxDB, just print the JSON output").Short('n').Bool()
	oneshot  = kingpin.Flag("oneshot", "Run once in the forground and exit").Short('o').Bool()
	debug    = kingpin.Flag("debug", "Print debug output").Short('d').Bool()
	host     = kingpin.Flag("host", "InfluxDB host to connect to").Default("localhost:8086").Short('h').String()
	username = kingpin.Flag("username", "InfluxDB username to authenticate as").Default("root").Short('u').String()
	password = kingpin.Flag("password", "Password to authenticate with").Default("root").Short('p').String()
	database = kingpin.Flag("database", "InfluxDB database to connect to").Short('D').String()
)

type ErrNonNumeric struct{ Msg string }

func (e *ErrNonNumeric) Error() string {
	return fmt.Sprintf("could not find a numeric value: %s", e.Msg)
}

type ErrPerfDataNotKeyValue struct {
	Msg     string
	LineNum int
}

func (e *ErrPerfDataNotKeyValue) Error() string {
	return fmt.Sprintf("perfdata found without a key = value format at Line %d: %s", e.LineNum, e.Msg)
}

type ErrNotPerfData struct {
	Msg     string
	LineNum int
}

func (e *ErrNotPerfData) Error() string {
	return fmt.Sprintf("perfdata is in unexpected format, not a single value or 5 \";\" separated string at Line %d: %s", e.LineNum, e.Msg)
}

//Structs
type Block struct {
	Section string
	Lines   []string
}

func prettyName(name []string) string {
	if name[1] == "" {
		return name[0]
	} else {
		return strings.Join(name, ".")
	}
}

// If value has a % convert to a decimal, otherwise strip any non-numerical suffixes
func parseDataValue(s string) (value interface{}, err error) {
	if s == "" {
		return s, err
	}
	if strings.HasSuffix(s, "%") {
		// Convert string percentage into float
		value := s[:len(s)-1]
		valFloat, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return s, err
		}
		// Return the decimal form of the percentage.
		return valFloat / 100, err
	} else {
		//Trim off the unit off any number
		re := regexp.MustCompile(`^-?\d+(\.\d+)?`)

		num := re.FindString(s)
		if num != "" {
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return s, err
			}
			return f, err
		} else {
			return s, &ErrNonNumeric{s}
		}
	}
}

// Parse host and service performance data. Return it in column name and values slices.
func parsePerfData(s string) (columns []string, points []interface{}, err error) {

	// Prefix all perfdata column names with:
	var prefix string = "performance_data."

	//Map to append to column name for perfdata values, "value;warn_limit;critical_limit;minimum_value;maximum value"
	var perfmap = map[int]string{
		0: "",
		1: ".warn",
		2: ".crit",
		3: ".min",
		4: ".max",
	}

	for _, perfdata := range strings.Split(s, " ") {
		perfdataKv := strings.Split(perfdata, "=")

		//fmt.Println(perfdataKv[0], perfdataKv[1])
		if len(perfdataKv) == 2 {
			valueAttrs := strings.Split(perfdataKv[1], ";")

			if len(valueAttrs) == 1 {
				columns = append(columns, strings.ToLower(prefix+string(perfdataKv[0])))
				points = append(points, perfdataKv[1])
			} else if len(valueAttrs) == 5 || len(valueAttrs) == 4 {
				//fmt.Println(perfdataKv[0], valueAttrs)
				for z, attr := range valueAttrs {
					if attr == "" {
						continue
					}
					value, err := parseDataValue(attr)
					if err != nil {
						log.Printf("Warning, parseDataValue: %s", err)
						continue
					}
					columns = append(columns, strings.ToLower(prefix+string(perfdataKv[0])+perfmap[z]))
					points = append(points, value)
				}
			} else {
				fmt.Println(perfdataKv[0], valueAttrs)
				err = &ErrPerfDataNotKeyValue{s, 0}
			}
		} else {
			err = &ErrNotPerfData{s, 0}
		}
	}
	return columns, points, err
}

func parseLine(line *string, inBlock *bool, section *string, block []string) []string {
	//Start of a block
	if strings.HasSuffix(*line, " {") {
		*inBlock = true
		*section = strings.TrimSuffix(*line, " {")
		//fmt.Printf("Start of block %s \n", *inBlock)
		//End of a block
	} else if *line == "\t}" {
		*inBlock = false
	} else if *inBlock == true {
		block = append(block, *line)
	}
	return block
}

func parseBlock(blockc chan Block, seriesc chan *client.Series, errc chan error) {

	for block := range blockc {

		var name []string = make([]string, 2)
		var columns []string = make([]string, 0, 60)
		var points []interface{} = make([]interface{}, 0, 60)

		for _, line := range block.Lines {
			//fmt.Println(block.Section)
			kv := strings.Split(line, "=")

			//Replace the last_check column with a column named time, so InfluxDB will use it as its index
			if kv[0] == "\tlast_check" {
				columns = append(columns, "time")
				time, err := strconv.Atoi(kv[1])
				if err != nil {
					errc <- err
				}
				points = append(points, time)
				continue
			} else if kv[0] == "\tperformance_data" && kv[1] != "" {
				perfColumns, perfValues, err := parsePerfData(strings.TrimPrefix(line, "\tperformance_data="))
				//TODO Print whole line instead of the specific perfdata, highlight specific perfdata
				if err != nil {
					errc <- err
				}
				//fmt.Println(perfColumns, perfValues)
				columns = append(columns, perfColumns...)
				points = append(points, perfValues...)
				continue
			}

			switch {
			case block.Section == "hoststatus":
				//fmt.Println(strings.TrimLeft(kv[0],"\t"))
				if kv[0] == "\thost_name" {
					name[0] = kv[1]
				}
			case block.Section == "servicestatus":
				if kv[0] == "\thost_name" {
					name[0] = kv[1]
				} else if kv[0] == "\tcheck_command" {
					name[1] = kv[1]
				}
				//fmt.Println(strings.TrimLeft(kv[0],"\t"))

			case block.Section == "info":
				name[0] = "info"
				//TODO: Figure out which field to use for time
			case block.Section == "programstatus":
				name[0] = "programstatus"
				//TODO: Figure out which field to use for time

			case block.Section == "contactstatus":
				if kv[0] == "\tcontact_name" {
					name[0] = kv[1]
				}
			case block.Section == "hostcomment" || block.Section == "servicecomment" || block.Section == "hostdowntime":
				if kv[0] == "\thost_name" {
					name[0] = kv[1]
					name[1] = block.Section
				}
			}

			if kv[1] != "" {
				value, _ := parseDataValue(kv[1])
				columns = append(columns, strings.TrimLeft(kv[0], "\t"))
				points = append(points, value)
			}
		}
		series := &client.Series{
			Name:    prettyName(name),
			Columns: columns,
			Points: [][]interface{}{
				points,
			},
		}

		if *noop == true {
			b, _ := json.MarshalIndent(series, "", "  ")
			b = append(b, "\n"...)
			os.Stdout.Write(b)
		} else {
			seriesc <- series
		}
	}
}

func uploader(c *client.Client, seriesc chan *client.Series, errc chan error) {

	for series := range seriesc {
		if err := c.WriteSeriesWithTimePrecision([]*client.Series{series}, client.Second); err != nil {
			errc <- err
		}
	}
}

func reader(blockc chan Block, filec chan *os.File) {
	for file := range filec {
		var inBlock bool
		var section string
		var block []string = make([]string, 0, 55)

		// Start reading the file
		readline.ReadLine(file, func(line string) {
			if string(line[0]) == "#" {
				if *debug {
					log.Printf("Skipped comment: %s", line)
				}
				return
			}

			block = parseLine(&line, &inBlock, &section, block)

			//Finished with a block
			if inBlock == false {
				blockc <- Block{section, block}

				//Empty the block slice and section since we're headed into a new section.
				block = make([]string, 0, 55)
				section = ""
			}
		})
	}

}

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

	var filec chan *os.File = make(chan *os.File)
	var blockc chan Block = make(chan Block)
	var seriesc chan *client.Series = make(chan *client.Series)
	var errc chan error = make(chan error, 10)

	// Startup an err channel handler
	go func() {
		for err := range errc {
			log.Printf("Error, %s", err)
		}
	}()

	var wgReader sync.WaitGroup
	var wgUploaders sync.WaitGroup
	var wgBlockParsers sync.WaitGroup

	wgReader.Add(1)
	go func() {
		reader(blockc, filec)
		wgReader.Done()
	}()

	filec <- file

	// Startup the uploader handlers
	wgUploaders.Add(numUploaders)
	for i := 0; i < numUploaders; i++ {
		go func() {
			uploader(c, seriesc, errc)
			wgUploaders.Done()
		}()
	}

	// Startup the block parsers
	wgBlockParsers.Add(numBlockParsers)
	for i := 0; i < numBlockParsers; i++ {
		go func() {
			parseBlock(blockc, seriesc, errc)
			wgBlockParsers.Done()
		}()
	}

	//The following needs to be in this very specfic order of closes and waits.
	close(filec)
	wgReader.Wait()
	//Close blockc so parseBlock will exit
	close(blockc)
	wgBlockParsers.Wait()
	//Close seriesc so uploader will exit
	close(seriesc)
	wgUploaders.Wait()
	close(errc)

	if err = file.Close(); err != nil {
		log.Fatalf("os.Close: %s", err)
	}
}
