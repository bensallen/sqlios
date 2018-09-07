package nagios

import (
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
