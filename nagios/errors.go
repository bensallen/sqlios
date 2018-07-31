package nagios

import "fmt"

//Custom Errors
type errNonNumeric struct{ Msg string }

func (e *errNonNumeric) Error() string {
	return fmt.Sprintf("could not find a numeric value: %s", e.Msg)
}

type errPerfDataNotKeyValue struct{ Msg string }

func (e *errPerfDataNotKeyValue) Error() string {
	return fmt.Sprintf("perfdata found without a key = value format: %s", e.Msg)
}

type errNotPerfData struct{ Msg string }

func (e *errNotPerfData) Error() string {
	return fmt.Sprintf("perfdata is in unexpected format, not a single value or 5 \";\" separated string: %s", e.Msg)
}
