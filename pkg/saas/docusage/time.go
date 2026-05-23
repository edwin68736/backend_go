package docusage

import "time"

const limaTZ = "America/Lima"

var limaLoc *time.Location

func lima() *time.Location {
	if limaLoc != nil {
		return limaLoc
	}
	loc, err := time.LoadLocation(limaTZ)
	if err != nil {
		limaLoc = time.FixedZone("America/Lima", -5*3600)
	} else {
		limaLoc = loc
	}
	return limaLoc
}

func nowLima() time.Time {
	return time.Now().In(lima())
}

func calendarDateLima(t time.Time) time.Time {
	lt := t.In(lima())
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, lima())
}
