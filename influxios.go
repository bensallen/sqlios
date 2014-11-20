package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/influxdb/influxdb-go"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

//Custom Errors
type ErrNonNumeric string

func (e ErrNonNumeric) Error() string {
	return fmt.Sprintf("could not find a numeric value: %s", e)
}

type ErrNotPerfDataKV string

func (e ErrNotPerfDataKV) Error() string {
	return fmt.Sprintf("perfdata found without a key = value format: %s", e)
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
var input = flag.String("input", "", "input file")

func startup() {
	flag.Parse()

	if *input == "" {
		log.Fatal("--input was not specified")
	}
	
	return
}

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
					columns = append(columns, strings.ToLower(prefix+string(perfdataKv[0])+perfmap[z]))
					value, err := parseDataValue(attr)
					if err != nil {
						log.Fatalf("parseDataValue: %s", err)
					}
					values = append(values, value)
				}
			} else {
				err = ErrNotPerfDataKV(s)
			}
		} else {
			err = ErrNotPerfData(s)
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
		
		// Name will be HOSTNAME.SERVICECHECKCOMMAND
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
				perfColumns, perfValues, _ := parsePerfData(kv[1])
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

	startup()
	
	lines, err := readLines(*input)

	if err != nil {
		log.Fatalf("readLines: %s", err)
	}

	for i, line := range lines {
		columns, values, name, err := parseLine(i, line)
		if err != nil {
			log.Printf("Warning, parseLine: %s", err)
		}

		series := &influxdb.Series{
			Name:    strings.Join(name, "."),
			Columns: columns,
			Points: [][]interface{}{
				values,
			},
		}
		b, _ := json.MarshalIndent(series, "", "  ")

		os.Stdout.Write(b)

		/*if err := client.WriteSeriesWithTimePrecision([]*influxdb.Series{series}, influxdb.Second); err != nil {
			log.Printf("influxdb.WriteSeries: %s", err)
		}*/
	}

}
