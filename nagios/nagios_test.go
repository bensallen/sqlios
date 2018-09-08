package nagios

import (
	"reflect"
	"testing"
)

func Test_trimUnit(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name    string
		args    args
		want    interface{}
		wantErr bool
	}{
		{
			name: "String",
			args: args{s: "asdfasdf"},
			want: "asdfasdf",
		},
		{
			name: "1",
			args: args{s: "1"},
			want: 1.0,
		},
		{
			name: "1%",
			args: args{s: "1%"},
			want: 1.0,
		},
		{
			name: "%1",
			args: args{s: "%1"},
			want: "%1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := trimUnit(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("trimUnit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("trimUnit() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func Test_parseDataValue(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name    string
		args    args
		want    interface{}
		wantErr bool
	}{
		{
			name: "100%",
			args: args{s: "100%"},
			want: 1.0,
		},
		{
			name: "100",
			args: args{s: "100"},
			want: 100.0,
		},
		{
			name: "100.0",
			args: args{s: "100.0"},
			want: 100.0,
		},
		{
			name: "100.0%",
			args: args{s: "100.0%"},
			want: 1.0,
		},
		{
			name: "100sec",
			args: args{s: "100sec"},
			want: 100.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDataValue(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDataValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseDataValue() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func Test_parseLine(t *testing.T) {
	type args struct {
		line    string
		inBlock bool
		section string
		block   []string
	}
	tests := []struct {
		name string
		args args
		want args
	}{
		{
			name: "Start of info block",
			args: args{
				line:    "info {",
				inBlock: false,
				section: "",
				block:   []string{},
			},
			want: args{
				inBlock: true,
				section: "info",
				block:   []string{},
			},
		},
		{
			name: "End of block",
			args: args{
				line:    "\t}",
				inBlock: true,
				section: "info",
				block:   []string{},
			},
			want: args{
				inBlock: false,
				section: "info",
				block:   []string{},
			},
		},
		{
			name: "Line in block",
			args: args{
				line:    "\tnagios_pid=5246",
				inBlock: true,
				section: "info",
				block:   []string{},
			},
			want: args{
				inBlock: true,
				section: "info",
				block:   []string{"\tnagios_pid=5246"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLine(&tt.args.line, &tt.args.inBlock, &tt.args.section, tt.args.block)

			// parseLine modifies the passed args inBlock, section, and returns a modified copy of block.
			if tt.args.inBlock != tt.want.inBlock || tt.args.section != tt.want.section || !reflect.DeepEqual(got, tt.want.block) {
				// Assign got to tt.args.block just to make printing below easier since block is returned as a copy.
				tt.args.block = got
				t.Errorf("parseLine() = %#v, want %#v", tt.args, tt.want)
			}
		})
	}
}
