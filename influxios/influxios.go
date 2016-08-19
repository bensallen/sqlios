package influxios

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/fsnotify/fsnotify"
	"github.com/influxdata/influxdb/client/v2"
	"github.com/jprichardson/readline-go"
	"gitlab.alcf.anl.gov/bsallen/watcher/watch"
)

//Joins the name slice with a "." if both elements exist, otherwise return just the first element
func prettyName(name []string) string {
	if name[1] == "" {
		return name[0]
	}
	return strings.Join(name, ".")
}

func trimUnit(s string) (interface{}, error) {
	for i := len(s) - 1; i >= 0; i-- {
		//fmt.Println(string(s[i]))
		if unicode.IsDigit(rune(s[i])) {
			f, err := strconv.ParseFloat(s[:i+1], 64)
			if err != nil {
				return s, nil
			}
			return f, err
		}
	}
	return s, nil
}

// If value has a % convert to a decimal, otherwise strip any non-numerical suffixes
func parseDataValue(s string) (interface{}, error) {
	if s == "" {
		return s, nil
	}
	if strings.HasSuffix(s, "%") {
		// Convert string percentage into float
		value := s[:len(s)-1]
		valFloat, _ := strconv.ParseFloat(value, 64)
		return valFloat / 100, nil
	}

	v, err := trimUnit(s)
	return v, err
}

// Parse host and service performance data. Add the key value data to the passed fields map pointer.
func parsePerfData(s string, fields *map[string]interface{}) error {

	// Prefix all perfdata field names with:
	var prefix = "performance_data."

	//Map to append to field name for perfdata values, "value;warn_limit;critical_limit;minimum_value;maximum value"
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
				(*fields)[strings.ToLower(prefix+string(perfdataKv[0]))] = perfdataKv[1]
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
					(*fields)[strings.ToLower(prefix+string(perfdataKv[0])+perfmap[z])] = value
				}
			} else {
				fmt.Println(perfdataKv[0], valueAttrs)
				return &errPerfDataNotKeyValue{s}
			}
		} else {
			return &errNotPerfData{s}
		}
	}
	return nil
}

func parseLine(line *string, inBlock *bool, section *string, block []string) []string {
	//Start of a block
	if strings.HasSuffix(*line, " {") {
		*inBlock = true
		*section = strings.TrimSuffix(*line, " {")
		//fmt.Printf("Start of block %t \n", *inBlock)
		//End of a block
	} else if *line == "\t}" {
		*inBlock = false
	} else if *inBlock == true {
		block = append(block, *line)
	}
	return block
}

func parseInfoBlock(block *Block) (err error) {

	if block.Created != 0 {
		block.LastCreated = block.Created
	}
	for _, line := range block.Lines {
		kv := strings.Split(line, "=")
		if kv[0] == "\tcreated" {
			currentCreated, err := strconv.ParseInt(kv[1], 10, 64)
			block.Created = currentCreated
			return err
		}
	}
	return
}

// ParseBlock parses Blocks into InfluxDB *client.Point
func ParseBlock(blockc chan Block, pointc chan *client.Point, errc chan error) {

	for block := range blockc {

		var name = make([]string, 2)
		var fields = make(map[string]interface{})
		var blockTime int64
		var skip bool

		for _, line := range block.Lines {

			kv := strings.Split(line, "=")

			// Parse the various time columns from status.dat into a int64
			if kv[0] == "\tlast_check" || kv[0] == "\tcreated" || kv[0] == "\tentry_time" {

				var err error
				blockTime, err = strconv.ParseInt(kv[1], 10, 64)
				if err != nil {
					errc <- err
				}

				//fmt.Printf("Last Created: %d, Item's Time: %d, UnixTime: %s\n", block.LastCreated, blockTime, time.Unix(blockTime, 0).String())

				// Compare this block's time to the last status.dat's created time.
				// We care about blocks that are newer than the last created time to
				// avoid uploading duplicate data points between status.dat updates.
				if block.LastCreated > blockTime {
					skip = true
					break
				}
				continue
			} else if kv[0] == "\tperformance_data" && kv[1] != "" {
				err := parsePerfData(strings.TrimPrefix(line, "\tperformance_data="), &fields)

				if err != nil {
					errc <- err
				}
				continue
			}

			const hostNameMatch string = "\thost_name"

			switch {
			case block.Name == "hoststatus":

				if kv[0] == hostNameMatch {
					name[0] = kv[1]
				}
			case block.Name == "servicestatus":
				if kv[0] == hostNameMatch {
					name[0] = kv[1]
				} else if kv[0] == "\tcheck_command" {
					name[1] = kv[1]
				}

			case block.Name == "info":
				name[0] = "info"

			//TODO: Figure out which field to use for time, for now skip programstatus
			case block.Name == "programstatus":
				skip = true
				break
			//name[0] = "programstatus"

			//TODO: Figure out which field to use for time, for now skip contactstatus
			case block.Name == "contactstatus":
				skip = true
				break
			//if kv[0] == "\tcontact_name" {
			//	name[0] = kv[1]
			//}
			case block.Name == "hostcomment" || block.Name == "servicecomment" || block.Name == "hostdowntime":
				if kv[0] == hostNameMatch {
					name[0] = kv[1]
					name[1] = block.Name
				}
			}

			if kv[1] != "" {
				value, err := parseDataValue(kv[1])
				if err != nil {
					errc <- err
				}
				fields[strings.TrimLeft(kv[0], "\t")] = value
			}
		}

		if skip {
			continue
		}
		unixTime := time.Unix(blockTime, 0)

		//fmt.Printf("time: %#v, fields: %#v\n", unixTime, fields)
		point, err := client.NewPoint(prettyName(name), map[string]string{}, fields, unixTime)
		if err != nil {
			errc <- err
		}
		//fmt.Printf("unixTime: %s, blockTime: %d, name: %s, tags: %s\n", unixTime.String(), blockTime, point.Name(), point.Tags())

		pointc <- point

	}
}

// Uploader takes Series from ParseBlock and either outputs Marshal'ed JSON when no-op'ed
// or pushes the series to InfluxDB
func Uploader(noop *bool, jsonOut *bool, c client.Client, pointc chan *client.Point, endOfFile chan bool, errc chan error) {
	var count int64

	//TODO Add this func to only run when verbose
	go func() {
		for range endOfFile {

			log.Printf("Uploaded %d points (est.)", count)
			count = 0
		}
	}()

	for point := range pointc {
		//fmt.Printf("time: %s, name: %s, tags: %s\n", point.Time().String(), point.Name(), point.Tags())
		count++
		if *jsonOut {
			b, _ := json.MarshalIndent(point.Fields(), "", "  ")
			b = append(b, "\n"...)
			os.Stdout.Write(b)
		}

		//if *noop == false {
		//	if err := c.WriteSeriesWithTimePrecision([]*client.Series{series}, client.Second); err != nil {
		//		errc <- err
		//	}
		//}
	}
}

// Reader reads files pushed to the filec channel, and outputs Block structs
func Reader(blockc chan Block, filec chan *os.File, endOfFile chan bool, errc chan error) {

	var lastCreated int64
	var currentCreated int64

	for file := range filec {
		var inBlock bool
		var firstBlock = true
		var name string
		var lines = make([]string, 0, 55)
		var count int64

		//TODO Add to verbose log level
		log.Print("Starting to read new file")

		// Start reading the file
		readline.ReadLine(file, func(line string) {

			if string(line[0]) == "#" {
				/*if *debug {
					log.Printf("Skipped comment: %s", line)
				}*/
				return
			}

			lines = parseLine(&line, &inBlock, &name, lines)
			//log.Printf("Lines: %s", lines)
			//Finished with a block
			if inBlock == false {

				// We're assuming the first block is the info block, we need to
				// update currentCreated and lastCreated before we move on.
				if firstBlock {

					infoBlock := &Block{name, lines, currentCreated, lastCreated}
					err := parseInfoBlock(infoBlock)
					if err != nil {
						log.Fatalf("Parsing Info Block failed, exiting: %s", err)
					}
					lastCreated = infoBlock.LastCreated
					currentCreated = infoBlock.Created

					//TODO add to verbose level
					log.Printf("Info block parsed: lastCreated time %d, currentCreated time %d", lastCreated, currentCreated)

					firstBlock = false
				}
				count++
				blockc <- Block{name, lines, currentCreated, lastCreated}

				//Empty the lines slice and name string since we're headed into a new block.
				lines = make([]string, 0, 55)
				name = ""
			}
		})

		log.Printf("Read in %d items", count)
		endOfFile <- true

		err := file.Close()
		if err != nil {
			errc <- err
		}
	}

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

	watch.Watch(path, *input, eventc, errc)

	//Wait until someone tells us we're done
	<-done
}
