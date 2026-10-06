package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

type zhLogger struct {
	slow  time.Duration
	level logger.LogLevel
}

func newZhLogger() logger.Interface {
	return zhLogger{
		slow:  200 * time.Millisecond,
		level: logger.Warn,
	}
}

func (l zhLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.level = level
	return l
}

func (l zhLogger) Info(_ context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Info {
		log.Printf(translateGorm(msg), data...)
	}
}

func (l zhLogger) Warn(_ context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Warn {
		log.Printf(translateGorm(msg), data...)
	}
}

func (l zhLogger) Error(_ context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Error {
		log.Printf(translateGorm(msg), data...)
	}
}

func (l zhLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	ms := float64(elapsed.Nanoseconds()) / 1e6
	switch {
	case err != nil && l.level >= logger.Error && !errors.Is(err, logger.ErrRecordNotFound):
		sql, rows := fc()
		log.Printf("%s 数据库错误 %s\n[%.3fms] [行数:%s] %s", utils.FileWithLineNum(), err, ms, rowsText(rows), sql)
	case err != nil && l.level >= logger.Error && errors.Is(err, logger.ErrRecordNotFound):
		sql, rows := fc()
		log.Printf("%s 未找到记录\n[%.3fms] [行数:%s] %s", utils.FileWithLineNum(), ms, rowsText(rows), sql)
	case elapsed > l.slow && l.slow != 0 && l.level >= logger.Warn:
		sql, rows := fc()
		log.Printf("%s 慢查询，耗时超过 %s\n[%.3fms] [行数:%s] %s", utils.FileWithLineNum(), l.slow, ms, rowsText(rows), sql)
	case l.level >= logger.Info:
		sql, rows := fc()
		log.Printf("%s\n[%.3fms] [行数:%s] %s", utils.FileWithLineNum(), ms, rowsText(rows), sql)
	}
}

func rowsText(rows int64) string {
	if rows == -1 {
		return "-"
	}
	return fmt.Sprintf("%d", rows)
}

func translateGorm(msg string) string {
	switch msg {
	case "failed to initialize database, got error %v":
		return "初始化数据库失败: %v"
	case "Got error when compile callbacks, got %v":
		return "编译回调失败: %v"
	case "removing callback `%s` from %s\n":
		return "正在移除回调 `%s`，位置 %s\n"
	case "replacing callback `%s` from %s\n":
		return "正在替换回调 `%s`，位置 %s\n"
	case "duplicated callback `%s` from %s\n":
		return "回调 `%s` 重复，位置 %s\n"
	case "The TranslateError option is enabled, but the Dialector %s does not implement ErrorTranslator.":
		return "已开启错误翻译，但驱动 %s 未实现错误翻译"
	case "failed to parse value %#v, got error %v":
		return "解析值 %#v 失败: %v"
	default:
		return msg
	}
}
