package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"os"
)

var Logger *zap.Logger

func DefaultLogger() {
	var allCore []zapcore.Core

	// High-priority output should also go to standard error, and low-priority
	// output should also go to standard out.
	consoleDebugging := zapcore.Lock(os.Stdout)

	// for human operators.
	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(cfg)
	// console
	allCore = append(allCore, zapcore.NewCore(consoleEncoder, consoleDebugging, zapcore.DebugLevel))
	core := zapcore.NewTee(allCore...)

	// From a zapcore.Core, it's easy to construct a Logger.
	Logger = zap.New(core).WithOptions(zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel), zap.AddCallerSkip(1), zap.Fields(zap.Int("pid", os.Getpid())))
	zap.ReplaceGlobals(Logger)
}

func InitLogger(fileName string, maxSize, maxBackups, maxAge int, compress bool) {
	var allCore []zapcore.Core

	infoLum := lumberjack.Logger{
		Filename:   fileName + ".info.log",
		MaxSize:    maxSize, // megabytes
		MaxBackups: maxBackups,
		MaxAge:     maxAge,   //days
		Compress:   compress, // disabled by default
	}

	infoFileWriter := zapcore.AddSync(&infoLum)

	errorLum := lumberjack.Logger{
		Filename:   fileName + ".error.log",
		MaxSize:    maxSize, // megabytes
		MaxBackups: maxBackups,
		MaxAge:     maxAge,   //days
		Compress:   compress, // disabled by default
	}

	errorFileWriter := zapcore.AddSync(&errorLum)

	// High-priority output should also go to standard error, and low-priority
	// output should also go to standard out.
	consoleDebugging := zapcore.Lock(os.Stdout)

	// for human operators.
	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(cfg)
	// console
	allCore = append(allCore, zapcore.NewCore(consoleEncoder, consoleDebugging, zapcore.DebugLevel))
	// error
	allCore = append(allCore, zapcore.NewCore(consoleEncoder, errorFileWriter, zapcore.ErrorLevel))
	// info
	allCore = append(allCore, zapcore.NewCore(consoleEncoder, infoFileWriter, zapcore.InfoLevel))
	core := zapcore.NewTee(allCore...)

	// From a zapcore.Core, it's easy to construct a Logger.
	Logger = zap.New(core).WithOptions(zap.AddCaller(), zap.AddCallerSkip(1), zap.Fields(zap.Int("pid", os.Getpid())))
	zap.ReplaceGlobals(Logger)
}

// Infof uses fmt.Sprintf to log a templated message.
func Infof(template string, args ...interface{}) {
	zap.S().Infof(template, args...)
}

func Debugf(template string, args ...interface{}) {
	zap.S().Debugf(template, args...)
}

func Errorf(template string, args ...interface{}) {
	zap.S().Errorf(template, args...)
}

func Warnf(template string, args ...interface{}) {
	zap.S().Warnf(template, args...)
}

func Fatalf(template string, args ...interface{}) {
	zap.S().Fatalf(template, args...)
}

func Info(msg string, fields ...zapcore.Field) {
	zap.L().Info(msg, fields...)
}

func Debug(msg string, fields ...zapcore.Field) {
	zap.L().Debug(msg, fields...)
}

func Error(msg string, fields ...zapcore.Field) {
	zap.L().Error(msg, fields...)
}

func Warn(msg string, fields ...zapcore.Field) {
	zap.L().Warn(msg, fields...)
}

func Fatal(msg string, fields ...zapcore.Field) {
	zap.L().Fatal(msg, fields...)
}
