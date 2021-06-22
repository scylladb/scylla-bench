package results

import "time"

// time.Duration objects are printed using time.Duration.String(), and that
// has hardcoded logic on the precision of its output, which is usually
// excessive. For example, durations more than a second are printed with
// 9 digits after the decimal point, and duration between 1ms and 1s
// are printed as ms with 6 digits after the decimal point.
// The Round function here rounds the duration in a way that printing it
// will result in up to "digits" digits after the decimal point.
func Round(d time.Duration) time.Duration {
	switch {
	case d < time.Microsecond:
		// Nanoseconds, no additional digits of precision
		d = d.Round(time.Nanosecond)
	case d < time.Millisecond:
		// Microseconds, no additional digits of precision
		d = d.Round(time.Microsecond)
	case d < time.Millisecond*time.Duration(10):
		// 1-10 milliseconds, show an additional digit of precision
		d = d.Round(time.Millisecond / time.Duration(10))
	case d < time.Second:
		// 10ms-1sec, show integer number of milliseconds.
		d = d.Round(time.Millisecond)
	default:
		// >1sec, show one additional digit of precision.
		d = d.Round(time.Second / time.Duration(10))
	}
	return d
}
