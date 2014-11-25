package main

import (
	"encoding/json"
	"flag"
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

// If value has a % convert to a decimal, otherwise strip any non-numerical suffixes
func parseDataValue(s string) (percent float64, err error) {
	if strings.HasSuffix(s, "%") {
		// Convert string percentage into float
		value := s[:len(s)-1]
		valFloat, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, err
		}
		// Return the decimal form of the percentage.
		return valFloat / 100, err
	} else {
		//Trim off the unit off any number
		re := regexp.MustCompile(`^-?\d+(\.\d+)?`)

		num := re.FindString(s)
		if num != "" {
			return strconv.ParseFloat(num, 64)
		} else {
			return 0, &ErrNonNumeric{s}
		}
	}
}

// Parse host and service performance data. Return it in column name and values slices.
func parsePerfData(s string) (columns []string, values []interface{}) {

	// Prefix all perfdata column names with:
	var prefix string = "perfdata."

	//Map to append to column name for perfdata values, "value;warn_limit;critical_limit;minimum_value;maximum value"
	var perfmap = map[int]string{
		0: "",
		1: ".warn",
		2: ".crit",
		3: ".min",
		4: ".max",
	}

	valueAttrs := strings.Split(s, ";")

	if len(valueAttrs) == 1 {
		columns = append(columns, strings.ToLower(prefix+string(perfdataKv[0])))
		values = append(values, perfdataKv[1])
	} else if len(valueAttrs) == 5 {
		//fmt.Println(i, valueAttrs)
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
			values = append(values, value)
		}
	}
	return columns, values
}

func parseLine(line *string, inBlock *string, name []string, columns []string, points []interface{}) ([]string, []string, []interface{}) {

	//Start of a block
	if strings.HasSuffix(*line, " {") {
		*inBlock = strings.TrimSuffix(*line, " {")

		//fmt.Printf("Start of block %s \n", *inBlock)
		//End of a block
	} else if strings.HasSuffix(*line, "}") {
		*inBlock = ""
	} else if *inBlock != "" {
		//log.Printf("Index at: %d", len(columns)-1)
		kv := strings.Split(*line, "=")

		//Replace the last_check column with a column named time, so InfluxDB will use it as the time value
		if kv[0] == "\tlast_check" {
			columns = append(columns, "time")
			time, err := strconv.Atoi(kv[1])
			if err != nil {
				log.Fatalf("strconv.Atoi: %s", err)
			}
			points = append(points, time)
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

		case *inBlock == "programstatus":

		}

		//fmt.Println(strings.TrimLeft(kv[0],"\t"))
		columns = append(columns, strings.TrimLeft(kv[0], "\t"))
		points = append(points, kv[1])
	}
	return name, columns, points
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

			series := &client.Series{
				Name:    strings.Join(name, "."),
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
			log.Print("Emptying columns and points slices")

			columns = make([]string, 0, 55)
			points = make([]interface{}, 0, 55)
		}
	})
}
