package influxios

// Block is made up of the section name from status.dat, lines of the block,
// Created time of the current status.dat file based on the info section
// and LastCreated time which is the created time from the last read status.dat
type Block struct {
	Name        string
	Lines       []string
	Created     int
	LastCreated int
}
