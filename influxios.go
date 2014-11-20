package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"github.com/influxdb/influxdb/client"
)

//Custom Errors
type ErrNonNumeric string

func (e ErrNonNumeric) Error() string {
	return fmt.Sprintf("could not find a numeric value: %s", e)
}

type ErrPerfDataNotKeyValue struct {
	Msg string
}

func (e *ErrPerfDataNotKeyValue) Error() string {
	return fmt.Sprintf("perfdata found without a key = value format: %s", e.Msg)
}

type ErrNotPerfData string

func (e ErrNotPerfData) Error() string {
	return fmt.Sprintf("perfdata is in unexpected format, not a single value or 5 \";\" separated string: %s", e)
}

type ErrColumnValueLengthMismatch string

// TODO Figure out how to include line number in error message.
func (e ErrColumnValueLengthMismatch) Error() string {
	return fmt.Sprintf("resulting columns and value element lengths are not equal: %s", e)
}

//Cmd line flags
var input 		= flag.String("input", "", "input file")
var noop  		= flag.Bool("noop", false, "Don't actually push any data to InfluxDB, just print the JSON output")
var host		= flag.String("host", "", "InfluxDB host to connect to")
var username	= flag.String("username", "", "InfluxDB username to authenticate as")
var password	= flag.String("password", "", "Password to authenticate with")
var database	= flag.String("database", "", "InfluxDB database to connect to")

// readLines reads a whole file into memory
// and returns a slice of its lines.
// TODO Figure out how to stream a file
func readLines(path string) (lines []string, err error) {
	var (
		file   *os.File
		part   []byte
		prefix bool
	)
	if file, err = os.Open(path); err != nil {
		return
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	buffer := bytes.NewBuffer(make([]byte, 0))
	for {
		if part, prefix, err = reader.ReadLine(); err != nil {
			break
		}
		buffer.Write(part)
		if !prefix {
			lines = append(lines, buffer.String())
			buffer.Reset()
		}
	}
	if err == io.EOF {
		err = nil
	}
	return
}

// Parse host and service performance data. Return it in column name and values slices.
func parsePerfData(s string) (columns []string, values []interface{}, err error) {

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

	for _, perfdata := range strings.Split(s, " ") {
		//fmt.Println(i, perfdata)
		perfdataKv := strings.Split(perfdata, "=")

		//fmt.Println(i, perfdata_kv[0], perfdata_kv[1])
		if len(perfdataKv) == 2 {
			valueAttrs := strings.Split(perfdataKv[1], ";")

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
			} else {
				err = &ErrPerfDataNotKeyValue{perfdata}
			}
		} else {
			err = ErrNotPerfData(perfdata)
		}
	}
	return columns, values, err
}

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
			return 0, ErrNonNumeric(s)
		}
	}
}

func parseLine(lineNum int, line string) (columns []string, values []interface{}, name []string, err error) {
	lineParts := strings.Split(line, "\t")

	// Sanity check, line should start with...
	if lineParts[0] == "DATATYPE::HOSTPERFDATA" || lineParts[0] == "DATATYPE::SERVICEPERFDATA" {
		
		// Series name will be HOSTNAME.SERVICECHECKCOMMAND
		name = make([]string, 2)
		
		for _, part := range lineParts {
			kv := strings.Split(part, "::")
			if kv[0] == "TIMET" {
				columns = append(columns, "time")
				time, err := strconv.Atoi(kv[1])
				if err != nil {
					log.Fatalf("strconv.Atoi: %s", err)
				}
				values = append(values, time)

			// Parse Perfdata
			} else if kv[0] == "SERVICEPERFDATA" || kv[0] == "HOSTPERFDATA" {
				perfColumns, perfValues, err := parsePerfData(kv[1])
				if err != nil {
					log.Panicf("Warning, parsePerfData: %s", err)
				}
				//fmt.Println(perfColumns, perfValues)
				columns = append(columns, perfColumns...)
				values = append(values, perfValues...)
			} else if kv[0] == "HOSTNAME" {
				name[0] = kv[1]
				columns = append(columns, strings.ToLower(kv[0]))
				values = append(values, strings.ToLower(kv[1]))
			} else if kv[0] == "SERVICECHECKCOMMAND" {
				name[1] = kv[1]
				columns = append(columns, strings.ToLower(kv[0]))
				values = append(values, strings.ToLower(kv[1]))
			} else {
				columns = append(columns, strings.ToLower(kv[0]))
				values = append(values, strings.ToLower(kv[1]))
			}
			//fmt.Println(i, kv[0], kv[1])
		}
		// Check that we are sane with matching number of columns and values
		if len(columns) == len(values) {
			//fmt.Println(i, columns, values)
			return columns, values, name, err
		} else {
			return columns, values, name, ErrColumnValueLengthMismatch(line)
		}

	}
	return columns, values, name, err
}

func main() {

	flag.Parse()

	if *input == "" {
		log.Fatal("--input was not specified")
	}
	
	lines, err := readLines(*input)

	if err != nil {
		log.Fatalf("readLines: %s", err)
	}

	c, err := client.NewClient(&client.ClientConfig{
		Host:       *host,
		Username:   *username,
		Password:   *password,
		Database:   *database,
	})
	
	if err != nil {
		log.Fatalf("client.NewClient: %s", err)
	}
	
	for i, line := range lines {
		columns, values, name, err := parseLine(i, line)
		if err != nil {
			log.Printf("Warning, parseLine: %s", err)
		}

		series := &client.Series{
			Name:    strings.Join(name, "."),
			Columns: columns,
			Points: [][]interface{}{
				values,
			},
		}
		
		if *noop == true {
			b, _ := json.MarshalIndent(series, "", "  ")
			os.Stdout.Write(b)
		} else {
			if err := c.WriteSeriesWithTimePrecision([]*client.Series{series}, client.Second); err != nil {
				log.Printf("client.WriteSeriesWithTimePrecision: %s", err)
			}
		}
	}

}
