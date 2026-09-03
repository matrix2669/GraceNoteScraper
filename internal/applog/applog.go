package applog

import (
	"log"
	"os"
)

var errorLogger = log.New(os.Stderr, "", log.LstdFlags)

func Errorf(format string, args ...any) {
	errorLogger.Printf("ERROR: "+format, args...)
}

func Warnf(format string, args ...any) {
	errorLogger.Printf("WARNING: "+format, args...)
}

func Fatalf(format string, args ...any) {
	errorLogger.Fatalf("FATAL: "+format, args...)
}
