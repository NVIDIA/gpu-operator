package reproducible

import (
	"errors"
	"os"
	"strconv"
	"time"
)

const EpocEnv = "SOURCE_DATE_EPOC"

var errInvalidEpoc = errors.New("invalid epoc var")

// TimeNow returns the current time or SOURCE_DATE_EPOC if that is set.
func TimeNow() time.Time {
	now, err := TimeEpocEnv()
	if err == nil {
		return now
	}
	return time.Now().UTC()
}

// TimeEpocEnv returns the time parsed by SOURCE_DATE_EPOC.
// This should be used to override any timestamps that should be reproducible.
func TimeEpocEnv() (time.Time, error) {
	sec := os.Getenv(EpocEnv)
	if sec == "" {
		return time.Time{}, errInvalidEpoc
	}
	secI, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(secI, 0).UTC(), nil
}
