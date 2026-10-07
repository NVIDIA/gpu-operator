package time

import "time"

// GetElapsedTime returns the difference in time between the endTime and startTime, and returns a human-readable duration
// as the output.
func GetElapsedTime(startTime, endTime time.Time) string {
	d := endTime.Sub(startTime).Truncate(time.Second)
	return d.String()
}
