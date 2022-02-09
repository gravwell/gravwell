package main

import (
	"errors"
	"fmt"
	"gravwell" //package expose the builtin plugin funcs
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	PluginName = "date_adjust"

	maxYear  int64 = 2199
	maxMonth int64 = 12
	maxDay   int64 = 31
)

var (
	cfg   DateConfig
	tg    gravwell.Tagger
	ready bool

	ErrNotReady = errors.New("not ready")
)

type DateConfig struct {
	Year  int
	Month int
	Day   int
}

func nop() error {
	return nil //this is a synchronous plugin, so no "start" or "close"
}

func Config(cm gravwell.ConfigMap, tgr gravwell.Tagger) (err error) {
	var val int64
	if cm == nil || tgr == nil {
		err = errors.New("bad parameters")
		return
	}
	if val, _ = cm.GetInt("year"); val != 0 {
		if val < 0 || val > maxYear {
			return errors.New("Invalid year")
		}
		cfg.Year = int(val)
	}
	if val, _ = cm.GetInt("month"); val != 0 {
		if val <= 0 || val > maxMonth {
			return errors.New("Invalid month")
		}
		cfg.Month = int(val)
	}
	if val, _ = cm.GetInt("day"); val != 0 {
		if val <= 0 || val > maxDay {
			return errors.New("Invalid day")
		}
		cfg.Day = int(val)
	}

	if cfg.Year == 0 && cfg.Month == 0 && cfg.Day == 0 {
		err = errors.New("At least one offset required")
	} else {
		tg = tgr
		ready = true
	}
	return
}

func Flush() []*entry.Entry {
	return nil //we don't hold on to anything
}

func Process(ents []*entry.Entry) ([]*entry.Entry, error) {
	if !ready {
		return nil, ErrNotReady
	}
	for i := range ents {
		var year, month, day int
		ts := ents[i].TS.StandardTime()
		if year = cfg.Year; year == 0 {
			year = ts.Year()
		}
		if month = cfg.Month; month == 0 {
			month = int(ts.Month())
		}
		if day = cfg.Day; day == 0 {
			day = int(ts.Day())
		}
		ents[i].TS = entry.FromStandard(time.Date(year, time.Month(month), day, ts.Hour(), ts.Minute(), ts.Second(), ts.Nanosecond(), ts.Location()))
	}
	return ents, nil
}

func main() {
	if err := gravwell.Execute(PluginName, Config, nop, nop, Process, Flush); err != nil {
		panic(fmt.Sprintf("Failed to execute dynamic plugin %s - %v\n", PluginName, err))
	}
}
