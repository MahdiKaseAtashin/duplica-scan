package model

import "time"

// FileMeta stores file attributes collected during scan.
type FileMeta struct {
	Name       string
	Path       string
	Size       int64
	ModifiedAt time.Time
}
