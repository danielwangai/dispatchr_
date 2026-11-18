// Package logging defines the application's logging boundary.
//
// Application packages depend on Logger and Field, rather than on a logging
// provider. Replacing Zerolog only requires another implementation here.
package logging

import (
	"io"
	"os"
	"time"

	"github.com/danielwangai/dispatchr/internal/config"

	"github.com/rs/zerolog"
)

// Logger is the provider-independent logger used by the application.
type Logger interface {
	Debug(message string, fields ...Field)
	Info(message string, fields ...Field)
	Error(message string, fields ...Field)
}

// Field is one structured value attached to a log entry.
type Field struct {
	Key   string
	Value any
}

func String(key, value string) Field  { return Field{Key: key, Value: value} }
func Int(key string, value int) Field { return Field{Key: key, Value: value} }
func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}
func Err(err error) Field { return Field{Key: "error", Value: err} }

// New creates the production JSON logger written to standard output.
func New(cfg *config.LoggingConfig) (Logger, error) {
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	return newZerolog(os.Stdout, level), nil
}

func newZerolog(output io.Writer, level zerolog.Level) Logger {
	logger := zerolog.New(output).With().Timestamp().Logger().Level(level)
	return &zerologLogger{logger: logger}
}

// Nop returns a logger that discards all messages. It is useful in tests.
func Nop() Logger { return newZerolog(io.Discard, zerolog.Disabled) }

type zerologLogger struct {
	logger zerolog.Logger
}

func (l *zerologLogger) Debug(message string, fields ...Field) {
	l.write(l.logger.Debug(), message, fields)
}

func (l *zerologLogger) Info(message string, fields ...Field) {
	l.write(l.logger.Info(), message, fields)
}

func (l *zerologLogger) Error(message string, fields ...Field) {
	l.write(l.logger.Error(), message, fields)
}

func (l *zerologLogger) write(event *zerolog.Event, message string, fields []Field) {
	for _, field := range fields {
		switch value := field.Value.(type) {
		case string:
			event.Str(field.Key, value)
		case int:
			event.Int(field.Key, value)
		case time.Duration:
			event.Dur(field.Key, value)
		case error:
			if field.Key == "error" {
				event.Err(value)
			} else {
				event.Interface(field.Key, value)
			}
		default:
			event.Interface(field.Key, value)
		}
	}
	event.Msg(message)
}
