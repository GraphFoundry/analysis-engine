package logger

import (
	"log"
	"os"
)

var (
	InfoLogger  = log.New(os.Stdout, "", 0)
	ErrorLogger = log.New(os.Stderr, "", 0)
)

func Info(msg string, fields map[string]interface{}) {

	InfoLogger.Printf("%s %v", msg, fields)
}

func Error(msg string, err error) {
	ErrorLogger.Printf("%s: %v", msg, err)
}
