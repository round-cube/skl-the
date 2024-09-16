package shared

import (
	log "github.com/sirupsen/logrus"
)

func PanicOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", err, msg)
	}
}

type UTCFormatter struct {
	log.Formatter
}

func (u UTCFormatter) Format(e *log.Entry) ([]byte, error) {
	e.Time = e.Time.UTC()
	return u.Formatter.Format(e)
}

func InitLog() {
	log.SetFormatter(UTCFormatter{&log.TextFormatter{DisableColors: true}})
	log.SetLevel(log.DebugLevel)
}
