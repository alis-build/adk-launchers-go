package storage

import "time"

// nowUnix returns the current Unix timestamp for result ids and creation times.
func nowUnix() int64 {
	return time.Now().Unix()
}
