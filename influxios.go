package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"regexp"
	//"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/influxdb/influxdb/client"
	"github.com/jprichardson/readline-go"
)

//Cmd line flags
var input = flag.String("input", "", "input file")
var noop = flag.Bool("noop", false, "Don't actually push any data to InfluxDB, just print the JSON output")
var host = flag.String("host", "", "InfluxDB host to connect to")
var username = flag.String("username", "", "InfluxDB username to authenticate as")
var password = flag.String("password", "", "Password to authenticate with")
var database = flag.String("database", "", "InfluxDB database to connect to")

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

func parseLine(line *string, inBlock *string, name []string, columns []string, points []interface{}) ([]string, []string, []interface{}) {

	//Start of a block
	if strings.HasSuffix(*line, " {") {
		*inBlock = strings.TrimSuffix(*line, " {")

		//fmt.Printf("Start of block %s \n", *inBlock)
		//End of a block
	} else if *line == "\t}" {
		*inBlock = ""
	} else if *inBlock != "" {
		//log.Printf("Index at: %d", len(columns)-1)
		kv := strings.Split(*line, "=")

		//Replace the last_check column with a column named time, so InfluxDB will use it as its index
		if kv[0] == "\tlast_check" {
			columns = append(columns, "time")
			time, err := strconv.Atoi(kv[1])
			if err != nil {
				log.Fatalf("strconv.Atoi: %s", err)
			}
			points = append(points, time)
			return name, columns, points
		} else if kv[0] == "\tperformance_data" && kv[1] != "" {
			perfColumns, perfValues, err := parsePerfData(strings.TrimPrefix(*line, "\tperformance_data="))
			//TODO Print whole line instead of the specific perfdata, highlight specific perfdata
			if err != nil {
				log.Printf("Warning, parsePerfData: %s", err)
			}
			//fmt.Println(perfColumns, perfValues)
			columns = append(columns, perfColumns...)
			points = append(points, perfValues...)
			return name, columns, points
		}

		switch {
		case *inBlock == "hoststatus":
			//fmt.Println(strings.TrimLeft(kv[0],"\t"))
			if kv[0] == "\thost_name" {
				name[0] = kv[1]
			}
		case *inBlock == "servicestatus":
			if kv[0] == "\thost_name" {
				name[0] = kv[1]
			} else if kv[0] == "\tcheck_command" {
				name[1] = kv[1]
			}
			//fmt.Println(strings.TrimLeft(kv[0],"\t"))

		case *inBlock == "info":
			name[0] = "info"
			//TODO: Figure out which field to use for time
		case *inBlock == "programstatus":
			name[0] = "programstatus"
			//TODO: Figure out which field to use for time

		case *inBlock == "contactstatus":
			if kv[0] == "\tcontact_name" {
				name[0] = kv[1]
			}
		case *inBlock == "hostcomment" || *inBlock == "servicecomment" || *inBlock == "hostdowntime":
			if kv[0] == "\thost_name" {
				name[0] = kv[1]
				name[1] = *inBlock
			}
		}

		//fmt.Println(strings.TrimLeft(kv[0],"\t"))
		if kv[1] != "" {
			value, _ := parseDataValue(kv[1])
			columns = append(columns, strings.TrimLeft(kv[0], "\t"))
			points = append(points, value)
		}
	}
	return name, columns, points
}

func prettyName(name []string) string {
	if name[1] == "" {
		return name[0]
	} else {
		return strings.Join(name, ".")
	}
}

func main() {

	flag.Parse()

	if *input == "" {
		log.Fatal("--input was not specified")
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

	var inBlock string
	var name []string = make([]string, 2)
	var columns []string = make([]string, 0, 55)
	var points []interface{} = make([]interface{}, 0, 55)

	readline.ReadLine(file, func(line string) {
		if string(line[0]) == "#" {
			log.Printf("Skipped comment: %s", line)
			return
		}

		name, columns, points = parseLine(&line, &inBlock, name, columns, points)

		if inBlock == "" {
			if prettyName(name) == "" {
				log.Fatalf("Empty name, bailing")
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
				if err := c.WriteSeriesWithTimePrecision([]*client.Series{series}, client.Second); err != nil {
					log.Printf("client.WriteSeriesWithTimePrecision: %s", err)
				}
			}

			//Empty the columns and points slices since we're into a new section.
			//log.Print("Emptying columns and points slices")

			name = make([]string, 2)
			columns = make([]string, 0, 55)
			points = make([]interface{}, 0, 55)
		}
	})

	err = file.Close()

	if err != nil {
		log.Fatalf("os.Close: %s", err)
	}
}
